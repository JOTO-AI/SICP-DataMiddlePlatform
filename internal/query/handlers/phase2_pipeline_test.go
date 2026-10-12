package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/handlers"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type phase2PipelineCase struct {
	op    contract.Operation
	body  string
	repo  *repository
	total int
}

func phase2PipelineCases() []phase2PipelineCase {
	return []phase2PipelineCase{
		{contract.Organizations, `{}`, &repository{tables: map[string][]source.SourceRow{organizations.Table: organizationRows()}}, 4},
		{contract.Students, `{}`, studentProfilesRepo(), 3},
		{contract.Staff, `{}`, staffRepo(), 3},
		{contract.StaffTeachingClasses, teacherClassesBody, moreTeachingClasses(), 3},
		{contract.TeachingClassStudents, classRosterBody, classRosterRepo(), 2},
	}
}
func dataObject(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var e struct{ Data map[string]any }
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	return e.Data
}

func TestT24_01FiveRealPipelinesColdHotFreshMetadataAndLogs(t *testing.T) {
	for _, tc := range phase2PipelineCases() {
		t.Run(string(tc.op), func(t *testing.T) {
			w := newWorld()
			w.tables = tc.repo.tables
			h, _ := pipelineSetup(t, w)
			cold := pipelineRequest(t.Context(), h, tc.op, tc.body, "synthetic-key", "")
			token := cold.Header().Get(snapshot.Header)
			if cold.Code != 200 || len(token) != 64 || cold.Header().Get("X-SICP-Combination") != pinned.Combination {
				t.Fatal(cold.Code, cold.Body.String())
			}
			data := dataObject(t, cold)
			if data["total"] != float64(tc.total) || data["synced_at"] != "2026-09-18T09:00:00+08:00" {
				t.Fatal(data)
			}
			w.mu.Lock()
			calls, trace := len(w.sql), len(w.trace)
			w.checks = w.checks.Add(time.Hour)
			w.mu.Unlock()
			rejected := pipelineRequest(t.Context(), h, tc.op, tc.body, "wrong", "bad")
			if rejected.Code != 401 {
				t.Fatal(rejected.Code)
			}
			w.mu.Lock()
			if len(w.sql) != calls || len(w.trace) != trace {
				t.Error("authentication touched query infrastructure")
			}
			w.mu.Unlock()
			hot := pipelineRequest(t.Context(), h, tc.op, tc.body, "synthetic-key", token)
			if hot.Code != 200 || hot.Header().Get(snapshot.Header) != token || hot.Header().Get("X-SICP-Combination") != pinned.Combination {
				t.Fatal(hot.Code, hot.Body.String())
			}
			hotData := dataObject(t, hot)
			if hotData["synced_at"] != "2026-09-18T10:00:00+08:00" {
				t.Fatal(hotData)
			}
			delete(data, "synced_at")
			delete(hotData, "synced_at")
			if !reflect.DeepEqual(data, hotData) {
				t.Fatal("cache changed business fields", data, hotData)
			}
			w.mu.Lock()
			defer w.mu.Unlock()
			if calls == 0 || len(w.sql) != calls || len(w.cache) != 1 || len(w.readers) != 1 || len(w.closed) != 1 || len(w.logs) != 9 {
				t.Fatal(w.trace, len(w.sql), len(w.logs))
			}
			for _, payload := range w.cache {
				if strings.Contains(string(payload), "synced_at") || strings.Contains(string(payload), "request_id") {
					t.Fatal(string(payload))
				}
			}
			for _, event := range w.logs {
				if event.Metadata.Operation != tc.op {
					t.Fatal(event.Metadata)
				}
			}
			for _, sql := range w.sql {
				if sql != pinned {
					t.Fatal(sql)
				}
			}
		})
	}
}

func TestT24_02FiveSnapshotsPinAllPagesAndRejectChangedBindings(t *testing.T) {
	for _, tc := range phase2PipelineCases() {
		t.Run(string(tc.op), func(t *testing.T) {
			w := newWorld()
			w.tables = tc.repo.tables
			h, _ := pipelineSetup(t, w)
			body := strings.TrimSuffix(tc.body, "}") + `,"page_size":1}`
			if tc.body == `{}` {
				body = `{"page_size":1}`
			}
			first := pipelineRequest(t.Context(), h, tc.op, body, "synthetic-key", "")
			token := first.Header().Get(snapshot.Header)
			if first.Code != 200 || len(token) != 64 {
				t.Fatal(first.Code, first.Body.String())
			}
			w.mu.Lock()
			next := pinned
			next.Combination = "C10"
			next.Generation++
			w.active = next
			w.readable[next] = true
			calls := len(w.sql)
			w.mu.Unlock()
			page2 := strings.TrimSuffix(body, "}") + `,"page":2}`
			second := pipelineRequest(t.Context(), h, tc.op, page2, "synthetic-key", token)
			if second.Code != 200 || second.Header().Get("X-SICP-Combination") != pinned.Combination || second.Header().Get(snapshot.Header) != token {
				t.Fatal(second.Code, second.Body.String())
			}
			w.mu.Lock()
			if len(w.sql) <= calls {
				t.Error("second page not loaded")
			}
			for _, sql := range w.sql {
				if sql != pinned {
					t.Error("active route leaked into continuation", sql)
				}
			}
			calls = len(w.sql)
			w.mu.Unlock()
			changed := strings.Replace(body, `"page_size":1`, `"page_size":2`, 1)
			bad := pipelineRequest(t.Context(), h, tc.op, changed, "synthetic-key", token)
			if bad.Code != 409 || bad.Header().Get(snapshot.Header) != "" || !strings.Contains(bad.Body.String(), `"data":null`) {
				t.Fatal(bad.Code, bad.Body.String())
			}
			other := contract.Organizations
			if tc.op == other {
				other = contract.Staff
			}
			bad = pipelineRequest(t.Context(), h, other, `{}`, "synthetic-key", token)
			if bad.Code != 409 {
				t.Fatal(bad.Code, bad.Body.String())
			}
			w.mu.Lock()
			if len(w.sql) != calls {
				t.Error("invalid token reached SQL")
			}
			w.readable[pinned] = false
			w.mu.Unlock()
			gone := pipelineRequest(t.Context(), h, tc.op, body, "synthetic-key", token)
			if gone.Code != 409 || gone.Header().Get("X-SICP-Combination") != "" {
				t.Fatal(gone.Code, gone.Body.String())
			}
		})
	}
}

func TestT24_03CompleteRegistrationDependenciesAndUncachedFailures(t *testing.T) {
	w := newWorld()
	var p handlers.Pipeline
	h, _ := pipelineSetup(t, w, func(got handlers.Pipeline) { p = got })
	ready := httptest.NewRecorder()
	h.ServeHTTP(ready, httptest.NewRequest("GET", "/readyz", nil))
	if ready.Code != 200 || len(contract.Definitions()) != 9 {
		t.Fatal(ready.Code)
	}
	recorder, err := events.New(testkit.FakeClock{Time: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"organizations source", "organizations clock", "students source", "students clock", "staff source", "staff clock", "teaching source", "teaching clock"} {
		copy := p
		switch missing {
		case "organizations source":
			copy.Organizations.Source = nil
		case "organizations clock":
			copy.Organizations.Clock = nil
		case "students source":
			copy.Students.Source = nil
		case "students clock":
			copy.Students.Clock = nil
		case "staff source":
			copy.Staff.Source = nil
		case "staff clock":
			copy.Staff.Clock = nil
		case "teaching source":
			copy.Teaching.Source = nil
		case "teaching clock":
			copy.Teaching.Clock = nil
		}
		if registered, err := copy.Handlers("synthetic-key", recorder, func(*events.Span, error) {}); err == nil || registered != nil {
			t.Fatal(missing, "partial pipeline registered")
		}
	}
	for _, tc := range phase2PipelineCases() {
		for _, kind := range []string{"cache get", "cache set", "SQL"} {
			t.Run(string(tc.op)+"/"+kind, func(t *testing.T) {
				w := newWorld()
				w.tables = tc.repo.tables
				failure := errors.New("private credentials")
				switch kind {
				case "cache get":
					w.getErr = failure
				case "cache set":
					w.setErr = failure
				case "SQL":
					w.sqlErr = failure
				}
				h, _ := pipelineSetup(t, w)
				got := pipelineRequest(t.Context(), h, tc.op, tc.body, "synthetic-key", "")
				status := 200
				if kind == "SQL" {
					status = 503
				}
				if got.Code != status || strings.Contains(got.Body.String(), "private credentials") {
					t.Fatal(got.Code, got.Body.String())
				}
				w.mu.Lock()
				defer w.mu.Unlock()
				if kind == "SQL" {
					if len(w.cache) != 0 || !strings.Contains(got.Body.String(), `"data":null`) {
						t.Fatal("failed result cached")
					}
				} else if w.observed == 0 {
					t.Fatal("cache failure unobserved")
				}
			})
		}
	}
	// Source consistency stays an internal error through the cache/coalescer path.
	w = newWorld()
	h, _ = pipelineSetup(t, w)
	bad := pipelineRequest(t.Context(), h, contract.Organizations, `{}`, "synthetic-key", "")
	if bad.Code != 500 {
		t.Fatal(bad.Code, bad.Body.String())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.cache) != 0 {
		t.Fatal("invalid graph cached")
	}
}
