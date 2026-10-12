package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/handlers"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

const teacherClassesBody = `{"staff_number":"T001","academic_year":"2026-2027","semester":"1"}`

func teachingHTTP(t *testing.T, repo *repository, limits read.Limits) (http.Handler, *admission) {
	t.Helper()
	svc, _ := teachingService(t, repo)
	svc.Limits = limits
	recorder, err := events.New(svc.Clock, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	a := &admission{}
	h, err := handlers.StaffTeachingClasses("synthetic-key", svc, a, metadata{operation: contract.StaffTeachingClasses}, recorder, func(*events.Span, error) {})
	if err != nil {
		t.Fatal(err)
	}
	return h, a
}
func teachingHTTPRequest(h http.Handler, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/sicp/staff/teaching-classes/query", strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func moreTeachingClasses() *repository {
	repo := teachingClassesRepo()
	repo.tables[timetable.SchedulesTable] = append(repo.tables[timetable.SchedulesTable], sourceRow(`{"PKH":"p3","JXBH":"001","JHKCH":"C1","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T001"}`), sourceRow(`{"PKH":"p4","JXBH":"J01","JHKCH":"C2","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T001"}`), sourceRow(`{"PKH":"p5","JXBH":"J01","JHKCH":"C2","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T004"}`))
	return repo
}

func TestT22_04ClassTotalAndTwoKeyPagination(t *testing.T) {
	for _, tc := range []struct {
		extra string
		total int
		keys  []string
	}{
		{``, 3, []string{"001/C1", "J01/C1", "J01/C2"}},
		{`,"page":2,"page_size":1`, 3, []string{"J01/C1"}},
		{`,"course_code":"C2"`, 1, []string{"J01/C2"}},
		{`,"page":4,"page_size":1`, 3, []string{}},
	} {
		repo := moreTeachingClasses()
		h, a := teachingHTTP(t, repo, read.DefaultLimits())
		w := teachingHTTPRequest(h, strings.TrimSuffix(teacherClassesBody, "}")+tc.extra+"}", "synthetic-key")
		var got struct {
			Data contract.StaffTeachingClassesResponse
		}
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		keys := []string{}
		for _, item := range got.Data.Items {
			keys = append(keys, item.TeachingClassNumber+"/"+item.PlannedCourseCode)
		}
		if w.Code != 200 || got.Data.Total != tc.total || !reflect.DeepEqual(keys, tc.keys) || got.Data.StaffNumber != "T001" || got.Data.AcademicYear != "2026-2027" || got.Data.Semester != "1" || got.Data.SyncedAt != "2026-09-18T09:00:00+08:00" || a.closed != 1 {
			t.Fatal(w.Code, w.Body.String(), a)
		}
	}
}

func TestT22_05DefaultHundredAndNormalEmpty(t *testing.T) {
	repo := &repository{tables: map[string][]source.SourceRow{}}
	for i := 0; i < 101; i++ {
		repo.tables[timetable.SchedulesTable] = append(repo.tables[timetable.SchedulesTable], sourceRow(fmt.Sprintf(`{"PKH":"p%03d","JXBH":"%03d","JHKCH":"C1","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T001"}`, i, i)))
	}
	h, _ := teachingHTTP(t, repo, read.DefaultLimits())
	w := teachingHTTPRequest(h, teacherClassesBody, "synthetic-key")
	var got struct {
		Data contract.StaffTeachingClassesResponse
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.Data.Total != 101 || len(got.Data.Items) != 100 || got.Data.Items[99].TeachingClassNumber != "099" {
		t.Fatal(w.Code, got.Data.Total, len(got.Data.Items))
	}
	body := strings.Replace(teacherClassesBody, "T001", "unknown", 1)
	w = teachingHTTPRequest(h, body, "synthetic-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestT22_06AnyCandidateFailureInvalidatesEvenFirstPage(t *testing.T) {
	for _, kind := range []string{"auth", "parse", "conflict", "empty conflict", "late name", "late SQL", "budget", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			repo := moreTeachingClasses()
			limits := read.DefaultLimits()
			body := strings.TrimSuffix(teacherClassesBody, "}") + `,"page_size":1}`
			key := "synthetic-key"
			status := 500
			switch kind {
			case "auth":
				key = "bad"
				status = 401
			case "parse":
				body = `{}`
				status = 400
			case "conflict", "empty conflict":
				repo.tables[timetable.SchedulesTable] = append(repo.tables[timetable.SchedulesTable], sourceRow(`{"PKH":"p6","JXBH":"J01","JHKCH":"C2","KKXND":"2025-2026","KKXQM":"1","JSJGH":"OTHER"}`))
				status = 409
				if kind == "empty conflict" {
					repo.tables[timetable.RelationsTable] = nil
				}
			case "late name":
				repo.tables[timetable.SchedulesTable][3]["JXBMC"] = json.RawMessage(`"one"`)
				repo.tables[timetable.SchedulesTable][4]["JXBMC"] = json.RawMessage(`"two"`)
			case "late SQL":
				repo.fail = timetable.RelationsTable
				status = 503
			case "budget":
				limits.Rows = 5
				status = 422
			case "cancel":
				status = 504
			}
			h, a := teachingHTTP(t, repo, limits)
			r := httptest.NewRequest("POST", "/", strings.NewReader(body))
			r.Header.Set("X-API-Key", key)
			if kind == "cancel" {
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				repo.cancel = cancel
				r = r.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != status || !strings.Contains(w.Body.String(), `"data":null`) || strings.Contains(w.Body.String(), "private SQL") {
				t.Fatal(w.Code, w.Body.String())
			}
			if kind == "auth" || kind == "parse" {
				if a.calls != 0 || len(repo.calls) != 0 {
					t.Fatal(a, repo.calls)
				}
			} else if a.calls != 1 || a.closed != 1 {
				t.Fatal(a)
			}
		})
	}
}
