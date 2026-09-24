package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type runtimeHealth struct {
	calls       int
	query, sync bool
}

func (h *runtimeHealth) Ready(context.Context) bool { panic("duplicate health probe") }
func (h *runtimeHealth) Status(context.Context) map[string]bool {
	h.calls++
	return map[string]bool{"query_ready": h.query, "sync_ready": h.sync, "dependencies_ready": true}
}
func TestT15_10RoleReadinessAndManagementRoutesAreIndependent(t *testing.T) {
	for _, role := range []config.Role{config.Query, config.Worker} {
		t.Run(string(role), func(t *testing.T) {
			health := &runtimeHealth{sync: true}
			var handlers map[contract.Operation]http.Handler
			if role == config.Query {
				handlers = map[contract.Operation]http.Handler{}
				for _, d := range contract.Definitions() {
					handlers[d.Operation] = http.NotFoundHandler()
				}
			}
			management := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
			h, e := newHTTP(role, "synthetic-query-key", testkit.FakeClock{}, testkit.FixedRandom(4), health, handlers, management)
			if e != nil {
				t.Fatal(e)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
			want := http.StatusServiceUnavailable
			if role == config.Worker {
				want = http.StatusOK
			}
			if w.Code != want || health.calls != 1 {
				t.Fatal(w.Code, health.calls)
			}
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", "/admin/initialize", nil))
			want = http.StatusNotFound
			if role == config.Worker {
				want = http.StatusAccepted
			}
			if w.Code != want {
				t.Fatal(w.Code)
			}
			h.Stop()
			w = httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
			var fields map[string]bool
			if e = json.Unmarshal(w.Body.Bytes(), &fields); e != nil || w.Code != http.StatusServiceUnavailable {
				t.Fatal(e, w.Code)
			}
			for k, value := range fields {
				if value {
					t.Fatal("draining readiness", k)
				}
			}
		})
	}
}
