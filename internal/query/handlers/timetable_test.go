package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const studentTimetableBody = `{"student_number":"001","academic_year":"2026-2027","semester":"1"}`

func timetableFixture() *repository {
	return &repository{tables: map[string][]source.SourceRow{
		timetable.RelationsTable: {sourceRow(`{"ID":"R1","XH":"001","JXBH":"001J","JHKCH":"001C"}`)},
		timetable.SchedulesTable: {
			sourceRow(`{"PKH":"P2","JXBH":"001J","JHKCH":"001C","KKXND":"2026-2027","KKXQM":"1","SKRQ":"20260921","ZC":"3","XQJ":"1","SKJC":"14","SKJSH":"002"}`),
			sourceRow(`{"PKH":"P1","JXBH":"001J","JHKCH":"001C","KKXND":"2026-2027","KKXQM":"1","SKRQ":"20260907","ZC":"1","XQJ":"星期一","SKJC":"1,2","SKJSH":"001","JSJGH":"T001,T002","JSXM":"示例教师","XDRS":0,"JXBMC":"示例教学班","JXDD":"示例地点","ETL_TIME":"2026-09-18 08:00:00"}`),
		},
	}}
}
func timetableRequest(h http.Handler, body, key string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/sicp/students/timetable/query", strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	h.ServeHTTP(w, r)
	return w
}

func TestT09_17StudentTimetableHTTPFullIndependentJSON(t *testing.T) {
	r := timetableFixture()
	h, a, log, _ := setupOperation(t, r, "", contract.StudentTimetable, read.DefaultLimits())
	w := timetableRequest(h, studentTimetableBody, "synthetic-key")
	want := `{"code":"ok","message":"OK","request_id":"01010101010101010101010101010101","data":{"student_number":"001","academic_year":"2026-2027","semester":"1","synced_at":"2026-09-18T09:00:00+08:00","items":[{"course_code":"001C","course_name":null,"planned_course_code":"001C","teaching_class_number":"001J","teaching_class_name":"示例教学班","enrolled_count":0,"teacher_staff_numbers":["T001","T002"],"room_code":"001","credits":null,"teacher_names":["示例教师"],"location":"示例地点","weekday":1,"sections":[1,2],"weeks":[1],"dates":["2026-09-07"],"source_updated_at":"2026-09-18T08:00:00+08:00"},{"course_code":"001C","course_name":null,"planned_course_code":"001C","teaching_class_number":"001J","teaching_class_name":null,"enrolled_count":null,"teacher_staff_numbers":[],"room_code":"002","credits":null,"teacher_names":[],"location":null,"weekday":1,"sections":[14],"weeks":[3],"dates":["2026-09-21"],"source_updated_at":null}]}}`
	var gotJSON, wantJSON any
	if json.Unmarshal(w.Body.Bytes(), &gotJSON) != nil || json.Unmarshal([]byte(want), &wantJSON) != nil || w.Code != 200 || !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.calls != 1 || a.closed != 1 || len(r.calls) != 3 || len(r.calls[1].filters) != 2 || !reflect.DeepEqual(r.calls[0].filters[0], source.Filter{Column: "XH", Value: "001"}) {
		t.Fatal(a, r.calls)
	}
	for _, call := range r.calls {
		if call.h != pinned {
			t.Fatal(call)
		}
	}
	if len(*log) != 3 || (*log)[0].Metadata.Operation != contract.StudentTimetable || (*log)[1].Outcome != events.Succeeded {
		t.Fatal(*log)
	}
	ready := httptest.NewRecorder()
	h.ServeHTTP(ready, httptest.NewRequest("GET", "/readyz", nil))
	if ready.Code != 503 {
		t.Fatal("staff handler still missing")
	}
}

func TestT09_16LaterPageFailureCancelAndBudgetNeverReturnPartialTimetable(t *testing.T) {
	for _, kind := range []string{"late-page", "course", "cancel", "budget", "ambiguity"} {
		t.Run(kind, func(t *testing.T) {
			r := timetableFixture()
			limits := read.DefaultLimits()
			want := 503
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "late-page":
				limits.BatchSize = 1
				r.failAt = 4
			case "course":
				r.fail = timetable.CoursesTable
			case "cancel":
				r.cancel = cancel
				want = 504
			case "budget":
				limits.Rows = 2
				want = 422
			case "ambiguity":
				r.tables[timetable.SchedulesTable][0]["KKXQM"] = json.RawMessage(`"2"`)
				want = 409
			}
			h, a, _, _ := setupOperation(t, r, "", contract.StudentTimetable, limits)
			req := httptest.NewRequest("POST", "/api/v1/sicp/students/timetable/query", strings.NewReader(studentTimetableBody)).WithContext(ctx)
			req.Header.Set("X-API-Key", "synthetic-key")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != want || !strings.Contains(w.Body.String(), `"data":null`) || strings.Contains(w.Body.String(), "001C") || strings.Contains(w.Body.String(), "private") || a.closed != 1 {
				t.Fatal(w.Code, w.Body.String(), a)
			}
			if kind == "late-page" && len(r.calls) != 4 {
				t.Fatal("not a later-page failure", r.calls)
			}
		})
	}
}

func TestT09_17EmptyAuthAndInputBoundaries(t *testing.T) {
	for _, tc := range []struct {
		body, key string
		status    int
	}{{studentTimetableBody, "synthetic-key", 200}, {studentTimetableBody, "wrong", 401}, {`{"student_number":"001"}`, "synthetic-key", 400}, {`{"student_number":"001","academic_year":"2026-2027","semester":"1","page":1}`, "synthetic-key", 400}} {
		r := &repository{}
		h, a, _, _ := setupOperation(t, r, "", contract.StudentTimetable, read.DefaultLimits())
		w := timetableRequest(h, tc.body, tc.key)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.status == 200 {
			if !strings.Contains(w.Body.String(), `"items":[]`) || strings.Contains(w.Body.String(), `"total"`) {
				t.Fatal(w.Body.String())
			}
		} else if a.calls != 0 || len(r.calls) != 0 {
			t.Fatal("invalid request reached source")
		}
	}
}
