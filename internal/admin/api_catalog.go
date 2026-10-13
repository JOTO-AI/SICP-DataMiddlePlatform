package admin

import (
	"net/http"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
)

func MountCatalog(mux *http.ServeMux, auth *Sessions, repo operations.Repository, runner *operations.TestRunner) error {
	routes := []struct {
		pattern string
		action  Action
		handler http.HandlerFunc
	}{
		{"GET /admin/v1/apis", Status, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			respond(w, 200, "ok", "", map[string]any{"version": operations.CatalogVersion, "items": operations.Catalog()})
		}},
		{"GET /admin/v1/apis/{id}", Status, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			item, ok := operations.LookupAPI(r.PathValue("id"))
			if !ok {
				operationError(w, operations.ErrMissing)
				return
			}
			respond(w, 200, "ok", "", item)
		}},
		{"POST /admin/v1/api-tests", APITest, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			if runner == nil {
				operationError(w, operations.ErrUnavailable)
				return
			}
			var body struct {
				operations.TestRequest
				IdempotencyKey string `json:"idempotency_key"`
			}
			if e := logquery.DecodeRequest(r.Body, &body); e != nil {
				logFailure(w, e)
				return
			}
			if e := body.Normalize(); e != nil {
				operationError(w, e)
				return
			}
			subject, _, _ := Identity(r.Context())
			j, e := repo.Accept(r.Context(), subject, "source_test", body.IdempotencyKey, body.TestRequest, time.Now().UTC(), 30*time.Second, operations.CheckSourceTest)
			if e != nil {
				operationError(w, e)
				return
			}
			w.Header().Set("Location", "/admin/v1/api-tests/"+j.ID)
			respond(w, 202, "accepted", "", j)
		}},
		{"GET /admin/v1/api-tests/{id}", APITest, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			subject, _, _ := Identity(r.Context())
			j, e := repo.Job(r.Context(), r.PathValue("id"), subject, "source_test")
			if e != nil {
				operationError(w, e)
				return
			}
			var payload any
			available := false
			if runner != nil {
				if raw, ok := runner.Results.Get(j.ID, subject, time.Now().UTC()); ok {
					payload = raw
					available = true
				}
			}
			state := "result_unavailable"
			if available {
				state = "available"
			}
			respond(w, 200, "ok", "", map[string]any{"job": j, "result_state": state, "result": payload})
		}},
	}
	for _, route := range routes {
		h, e := auth.Protect(route.action, route.handler)
		if e != nil {
			return e
		}
		mux.Handle(route.pattern, h)
	}
	return nil
}
