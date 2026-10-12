package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/handlers"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/students"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

const classRosterBody = `{"teaching_class_number":"J01","planned_course_code":"C1","academic_year":"2026-2027","semester":"1"}`

func classRosterHTTP(t *testing.T, repo *repository, limits read.Limits) (http.Handler, *admission) {
	t.Helper()
	svc, _ := teachingService(t, repo)
	svc.Limits = limits
	recorder, err := events.New(svc.Clock, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	a := &admission{}
	h, err := handlers.TeachingClassStudents("synthetic-key", svc, a, metadata{operation: contract.TeachingClassStudents}, recorder, func(*events.Span, error) {})
	if err != nil {
		t.Fatal(err)
	}
	return h, a
}
func classRosterHTTPRequest(h http.Handler, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/sicp/teaching-classes/students/query", strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestT23_04RosterPagesEqualTeacherClassCount(t *testing.T) {
	repo := classRosterRepo()
	teacher, _ := teachingHTTP(t, repo, read.DefaultLimits())
	tw := teachingHTTPRequest(teacher, teacherClassesBody, "synthetic-key")
	var count struct {
		Data contract.StaffTeachingClassesResponse
	}
	_ = json.Unmarshal(tw.Body.Bytes(), &count)
	if tw.Code != 200 || len(count.Data.Items) != 1 {
		t.Fatal(tw.Code, tw.Body.String())
	}
	h, a := classRosterHTTP(t, repo, read.DefaultLimits())
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		repo.calls = nil
		body := strings.TrimSuffix(classRosterBody, "}") + fmt.Sprintf(`,"page":%d,"page_size":1}`, page)
		w := classRosterHTTPRequest(h, body, "synthetic-key")
		var got struct {
			Data contract.TeachingClassStudentsResponse
		}
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if w.Code != 200 || got.Data.Total != 2 || got.Data.Total != count.Data.Items[0].StudentCount || got.Data.TeachingClassNumber != "J01" || got.Data.PlannedCourseCode != "C1" || got.Data.AcademicYear != "2026-2027" || got.Data.Semester != "1" || got.Data.SyncedAt != "2026-09-18T09:00:00+08:00" {
			t.Fatal(w.Code, w.Body.String())
		}
		if page <= 2 {
			want := "001"
			if page == 2 {
				want = "01"
			}
			if len(got.Data.Items) != 1 || got.Data.Items[0].StudentNumber != want || seen[want] {
				t.Fatal(got)
			}
			seen[want] = true
			for _, call := range repo.calls {
				if call.table == students.Table || call.table == students.EnrollmentsTable {
					if len(call.filters) != 1 || len(call.filters[0].Values) != 1 || call.filters[0].Values[0] != want {
						t.Fatal("off-page profile fetched", call)
					}
				}
			}
		} else if len(got.Data.Items) != 0 || !strings.Contains(w.Body.String(), `"items":[]`) || len(repo.calls) != 2 {
			t.Fatal(w.Body.String(), repo.calls)
		}
	}
	if len(seen) != count.Data.Items[0].StudentCount || a.calls != 3 || a.closed != 3 {
		t.Fatal(seen, count, a)
	}
}

func TestT23_05DefaultHundredAndConfirmedEmpty(t *testing.T) {
	repo := classRosterRepo()
	repo.tables[timetable.RelationsTable] = nil
	for i := 0; i < 101; i++ {
		repo.tables[timetable.RelationsTable] = append(repo.tables[timetable.RelationsTable], sourceRow(fmt.Sprintf(`{"ID":"r%03d","JXBH":"J01","JHKCH":"C1","XH":"%03d"}`, i, i)))
	}
	h, _ := classRosterHTTP(t, repo, read.DefaultLimits())
	w := classRosterHTTPRequest(h, classRosterBody, "synthetic-key")
	var got struct {
		Data contract.TeachingClassStudentsResponse
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.Data.Total != 101 || len(got.Data.Items) != 100 || got.Data.Items[99].StudentNumber != "099" {
		t.Fatal(w.Code, got.Data.Total, len(got.Data.Items))
	}
	repo.tables[timetable.RelationsTable] = nil
	w = classRosterHTTPRequest(h, classRosterBody, "synthetic-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestT23_06RosterErrorsAndOptionalAssociationFailures(t *testing.T) {
	for _, kind := range []string{"auth", "parse", "missing combination", "missing term", "conflict", "empty conflict", "missing student", "SQL", "bad profile", "budget", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			repo := classRosterRepo()
			limits := read.DefaultLimits()
			body := classRosterBody
			key := "synthetic-key"
			status := 500
			switch kind {
			case "auth":
				key = "bad"
				status = 401
			case "parse":
				body = `{}`
				status = 400
			case "missing combination":
				body = strings.Replace(body, "J01", "missing", 1)
				status = 400
			case "missing term":
				body = strings.Replace(body, `"semester":"1"`, `"semester":"2"`, 1)
				status = 400
			case "conflict", "empty conflict":
				repo.tables[timetable.SchedulesTable] = append(repo.tables[timetable.SchedulesTable], sourceRow(`{"PKH":"p3","JXBH":"J01","JHKCH":"C1","KKXND":"2025-2026","KKXQM":"1","JSJGH":"OTHER"}`))
				status = 409
				if kind == "empty conflict" {
					repo.tables[timetable.RelationsTable] = nil
				}
			case "missing student":
				repo.tables[timetable.RelationsTable][2]["XH"] = json.RawMessage(`null`)
				body = strings.TrimSuffix(body, "}") + `,"page_size":1}`
			case "SQL":
				repo.fail = students.ClassesTable
				status = 503
			case "bad profile":
				repo.tables[students.Table][0]["XM"] = json.RawMessage(`123`)
			case "budget":
				limits.Rows = 5
				status = 422
			case "cancel":
				status = 504
			}
			h, a := classRosterHTTP(t, repo, limits)
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
