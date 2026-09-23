package handlers_test

import (
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

const staffTimetableBody = `{"staff_number":"T001","academic_year":"2026-2027","semester":"1"}`

func staffRequestHTTP(h http.Handler, body, key string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/sicp/staff/timetable/query", strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	h.ServeHTTP(w, r)
	return w
}
func staffFixture() *repository {
	r := timetableFixture()
	delete(r.tables, timetable.RelationsTable)
	r.tables[timetable.SchedulesTable][0]["JSJGH"] = json.RawMessage(`"T001,T001"`)
	return r
}

func TestT10_10StaffHTTPHasCompleteIndependentResponseAndNoMembershipReads(t *testing.T) {
	r := staffFixture()
	h, a, log, _ := setupOperation(t, r, "", contract.StaffTimetable, read.DefaultLimits())
	w := staffRequestHTTP(h, staffTimetableBody, "synthetic-key")
	want := `{"code":"ok","message":"OK","request_id":"01010101010101010101010101010101","data":{"staff_number":"T001","academic_year":"2026-2027","semester":"1","synced_at":"2026-09-18T09:00:00+08:00","items":[{"course_code":"001C","course_name":null,"planned_course_code":"001C","teaching_class_number":"001J","teaching_class_name":"示例教学班","enrolled_count":0,"teacher_staff_numbers":["T001","T002"],"room_code":"001","credits":null,"teacher_names":["示例教师"],"location":"示例地点","weekday":1,"sections":[1,2],"weeks":[1],"dates":["2026-09-07"],"source_updated_at":"2026-09-18T08:00:00+08:00"},{"course_code":"001C","course_name":null,"planned_course_code":"001C","teaching_class_number":"001J","teaching_class_name":null,"enrolled_count":null,"teacher_staff_numbers":["T001"],"room_code":"002","credits":null,"teacher_names":[],"location":null,"weekday":1,"sections":[14],"weeks":[3],"dates":["2026-09-21"],"source_updated_at":null}]}}`
	var actual, expected any
	if json.Unmarshal(w.Body.Bytes(), &actual) != nil || json.Unmarshal([]byte(want), &expected) != nil || w.Code != 200 || !reflect.DeepEqual(actual, expected) {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.calls != 1 || a.closed != 1 || len(r.calls) != 2 || r.calls[0].table != timetable.SchedulesTable || !reflect.DeepEqual(r.calls[0].filters, []source.Filter{{Column: "KKXND", Value: "2026-2027"}, {Column: "KKXQM", Value: "1"}}) {
		t.Fatal(a, r.calls)
	}
	for _, call := range r.calls {
		if call.h != pinned || call.table == timetable.RelationsTable {
			t.Fatal(call)
		}
	}
	if len(*log) != 3 || (*log)[0].Metadata.Operation != contract.StaffTimetable || (*log)[1].Outcome != events.Succeeded {
		t.Fatal(*log)
	}
}

func TestT10_08StaffRepositoryFailureIsNotAnEmptyTimetable(t *testing.T) {
	for _, kind := range []string{"source", "later-page", "course", "budget"} {
		t.Run(kind, func(t *testing.T) {
			r := staffFixture()
			limits := read.DefaultLimits()
			want := 503
			switch kind {
			case "source":
				r.fail = timetable.SchedulesTable
			case "later-page":
				limits.BatchSize = 1
				r.failAt = 2
			case "course":
				r.fail = timetable.CoursesTable
			case "budget":
				limits.Rows = 1
				want = 422
			}
			h, a, _, _ := setupOperation(t, r, "", contract.StaffTimetable, limits)
			w := staffRequestHTTP(h, staffTimetableBody, "synthetic-key")
			if w.Code != want || !strings.Contains(w.Body.String(), `"data":null`) || strings.Contains(w.Body.String(), "001C") || strings.Contains(w.Body.String(), "private") || a.closed != 1 {
				t.Fatal(w.Code, w.Body.String(), a)
			}
		})
	}
}

func TestT10_10StaffHTTPFiltersEmptyAndAuthentication(t *testing.T) {
	for _, tc := range []struct {
		body  string
		count int
	}{
		{`{"staff_number":"T00","academic_year":"2026-2027","semester":"1"}`, 0},
		{`{"staff_number":"T001","academic_year":"2026-2027","semester":"2"}`, 0},
		{`{"staff_number":"T001","academic_year":"2026-2027","semester":"1","course_query":"001C","teaching_class_number":"001J"}`, 2},
		{`{"staff_number":"T001","academic_year":"2026-2027","semester":"1","course_query":"001C","teaching_class_number":"missing"}`, 0},
	} {
		r := staffFixture()
		h, _, _, _ := setupOperation(t, r, "", contract.StaffTimetable, read.DefaultLimits())
		w := staffRequestHTTP(h, tc.body, "synthetic-key")
		var envelope struct {
			Data struct {
				Items []json.RawMessage `json:"items"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || w.Code != 200 || len(envelope.Data.Items) != tc.count || strings.Contains(w.Body.String(), `"total"`) {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.count == 0 && !strings.Contains(w.Body.String(), `"items":[]`) {
			t.Fatal(w.Body.String())
		}
	}
	for _, tc := range []struct {
		body, key string
		status    int
	}{{staffTimetableBody, "wrong", 401}, {`{"staff_number":"T001"}`, "synthetic-key", 400}} {
		r := staffFixture()
		h, a, _, _ := setupOperation(t, r, "", contract.StaffTimetable, read.DefaultLimits())
		w := staffRequestHTTP(h, tc.body, tc.key)
		if w.Code != tc.status || a.calls != 0 || len(r.calls) != 0 {
			t.Fatal(w.Code, a, r.calls)
		}
	}
}
