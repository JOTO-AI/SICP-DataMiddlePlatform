package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/handlers"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/service"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

var pinned = read.Handle{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 9, Combination: "C09"}

type admission struct{ calls, closed int }

func (a *admission) Acquire(context.Context, string) (read.RequestLease, error) {
	a.calls++
	return a, nil
}
func (a *admission) Handle() read.Handle         { return pinned }
func (a *admission) Close(context.Context) error { a.closed++; return nil }

type metadata struct{}

func (metadata) SyncedAt(_ context.Context, h read.Handle, op contract.Operation) (time.Time, error) {
	if h != pinned || op != contract.StudentGrades {
		return time.Time{}, errors.New("wrong metadata handle")
	}
	return time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC), nil
}

type queryCall struct {
	h       read.Handle
	table   string
	filters []source.Filter
}
type repository struct {
	tables map[string][]source.SourceRow
	calls  []queryCall
	fail   string
}

func (r *repository) ReadMatching(_ context.Context, h read.Handle, table, after string, limit int, filters []source.Filter) ([]source.SourceRow, error) {
	r.calls = append(r.calls, queryCall{h, table, append([]source.Filter(nil), filters...)})
	if h != pinned {
		return nil, errors.New("wrong repository handle")
	}
	if table == r.fail {
		return nil, errors.New("private SQL credentials")
	}
	pk := ""
	for _, v := range source.Catalog() {
		if v.Name == table {
			pk = v.PrimaryKey
		}
	}
	if pk == "" {
		return nil, errors.New("unknown table")
	}
	result := []source.SourceRow{}
	for _, row := range r.tables[table] {
		id, _ := compute.Required(row, pk)
		if id <= after {
			continue
		}
		matches := true
		for _, f := range filters {
			v, _ := compute.Text(row, f.Column)
			if v == nil || *v != f.Value {
				matches = false
			}
		}
		if matches {
			result = append(result, row)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, _ := compute.Required(result[i], pk)
		b, _ := compute.Required(result[j], pk)
		return a < b
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func sourceRow(raw string) source.SourceRow {
	var row source.SourceRow
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		panic(err)
	}
	return row
}

func setup(t *testing.T, r *repository, failPhase events.Phase) (http.Handler, *admission, *[]events.Event, *int) {
	t.Helper()
	clock := testkit.FakeClock{Time: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}
	all := []events.Event{}
	observation := 0
	recorder, err := events.New(clock, testkit.FixedRandom(2), events.SinkFunc(func(_ context.Context, e events.Event) error {
		all = append(all, e)
		if e.Phase == failPhase {
			return errors.New("log failure")
		}
		return nil
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	a := &admission{}
	times, _ := compute.NewTimes("Asia/Shanghai")
	h, err := handlers.StudentGrades("synthetic-key", grades.Service{Source: r, Times: times, Clock: clock, Limits: read.DefaultLimits()}, a, metadata{}, recorder, func(*events.Span, error) { observation++ })
	if err != nil {
		t.Fatal(err)
	}
	httpService, err := service.NewHTTPWithHandlers(config.Query, "synthetic-key", clock, testkit.FixedRandom(1), testkit.ReadyFunc(func(context.Context) bool { return true }), map[contract.Operation]http.Handler{contract.StudentGrades: h})
	if err != nil {
		t.Fatal(err)
	}
	return httpService, a, &all, &observation
}
func request(h http.Handler, body, key string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/sicp/students/grades/query", strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	h.ServeHTTP(w, r)
	return w
}

func TestT07_17RealHTTPFullResponseAndPinnedSourceCalls(t *testing.T) {
	r := &repository{tables: map[string][]source.SourceRow{grades.GradesTable: {sourceRow(`{"ID":"G01","XH":"001","XN":"2025-2026","XQM":"2","KCH":"C01","XF":2,"KCCJ":0}`)}}}
	h, a, log, _ := setup(t, r, "")
	w := request(h, `{"student_number":"001","academic_year":"2025-2026","semester":"2","course_code":"C01","score_min":0,"score_max":0,"page":1,"page_size":1}`, "synthetic-key")
	want := `{"code":"ok","message":"OK","request_id":"01010101010101010101010101010101","data":{"total":1,"synced_at":"2026-09-18T09:00:00+08:00","items":[{"grade_id":"G01","student":{"student_number":"001","name":null,"department_code":null,"department_name":null,"class_code":null,"class_name":null},"academic_year":"2025-2026","semester":"2","course":{"course_code":"C01","course_name":null,"attribute_code":null,"attribute_name":null,"nature_code":null,"nature_name":null,"type_code":null,"type_name":null,"department":null,"total_hours":null,"retake_flag":null,"retake_course_code":null,"retake_course_name":null},"score":0,"grade_point":null,"credits":2,"source_updated_at":null}]}}`
	var gotJSON, wantJSON any
	_ = json.Unmarshal(w.Body.Bytes(), &gotJSON)
	_ = json.Unmarshal([]byte(want), &wantJSON)
	if w.Code != 200 || !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.calls != 1 || a.closed != 1 || len(r.calls) != 4 || r.calls[0].filters[0].Column != "XH" || r.calls[0].filters[0].Value != "001" {
		t.Fatal(a, r.calls)
	}
	for _, call := range r.calls {
		if call.h != pinned {
			t.Fatal(call)
		}
	}
	if len(*log) != 3 || (*log)[0].Metadata.Operation != contract.StudentGrades || (*log)[0].Identity.RequestID != w.Header().Get("X-Request-ID") || (*log)[1].Outcome != events.Succeeded || (*log)[2].Phase != events.Transport {
		t.Fatal("invalid lifecycle")
	}
	ready := httptest.NewRecorder()
	h.ServeHTTP(ready, httptest.NewRequest("GET", "/readyz", nil))
	if ready.Code != 503 {
		t.Fatal("partial implementation claimed readiness")
	}
}

func TestT07_17InvalidInputAndAuthCannotReachRepository(t *testing.T) {
	for _, tc := range []struct {
		body, key string
		status    int
	}{{`{"student_number":"001"}`, "wrong", 401}, {`{"student_number":1}`, "synthetic-key", 400}, {`{"student_number":"001","unknown":true}`, "synthetic-key", 400}} {
		r := &repository{}
		h, a, _, _ := setup(t, r, "")
		w := request(h, tc.body, tc.key)
		if w.Code != tc.status || a.calls != 0 || len(r.calls) != 0 {
			t.Fatal(w.Code, a, r.calls)
		}
	}
}

func TestT07_16LaterAssociationFailureDoesNotReturnPartialHTTPData(t *testing.T) {
	r := &repository{fail: grades.CoursesTable, tables: map[string][]source.SourceRow{grades.GradesTable: {sourceRow(`{"ID":"G01","XH":"001","XN":"2025-2026","XQM":"2","KCH":"C01"}`)}}}
	h, a, log, _ := setup(t, r, "")
	w := request(h, `{"student_number":"001"}`, "synthetic-key")
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"data":null`) || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "G01") || a.closed != 1 || (*log)[1].Outcome != events.Failed {
		t.Fatal(w.Code, w.Body.String(), a)
	}
}

func TestT07_17EmptyResponseAndLogFailureBoundaries(t *testing.T) {
	for _, phase := range []events.Phase{"", events.Started, events.Finished} {
		r := &repository{}
		h, a, _, observed := setup(t, r, phase)
		w := request(h, `{"student_number":"001"}`, "synthetic-key")
		if phase == events.Started {
			if w.Code != 503 || a.calls != 0 || len(r.calls) != 0 {
				t.Fatal(w.Code, a)
			}
			continue
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) || !strings.Contains(w.Body.String(), `"items":[]`) {
			t.Fatal(w.Code, w.Body.String())
		}
		if phase == events.Finished && *observed != 1 {
			t.Fatal("failed finish observation lost")
		}
	}
}

type failedWriter struct{ header http.Header }

func (w *failedWriter) Header() http.Header       { return w.header }
func (w *failedWriter) WriteHeader(int)           {}
func (w *failedWriter) Write([]byte) (int, error) { return 0, errors.New("client lost") }
func TestT07_17SuccessfulBusinessCanHaveInterruptedTransport(t *testing.T) {
	h, _, log, _ := setup(t, &repository{}, "")
	r := httptest.NewRequest("POST", "/api/v1/sicp/students/grades/query", strings.NewReader(`{"student_number":"001"}`))
	r.Header.Set("X-API-Key", "synthetic-key")
	h.ServeHTTP(&failedWriter{header: make(http.Header)}, r)
	if len(*log) != 3 || (*log)[1].Outcome != events.Succeeded || (*log)[2].Outcome != events.Interrupted {
		t.Fatal("transport failure rewrote business outcome")
	}
}
