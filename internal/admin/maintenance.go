package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func MountMaintenance(mux *http.ServeMux, auth *Sessions, m operations.Maintenance, status func(context.Context) map[string]any) error {
	routes := []struct {
		pattern string
		action  Action
		fn      http.HandlerFunc
	}{
		{"GET /admin/v1/system/status", SystemRead, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			out := status(ctx)
			out["retention"], out["retention_state"] = nil, "unavailable"
			if s, e := m.Repository.Retention(ctx); e == nil {
				out["retention"], out["retention_state"] = s, "available"
			}
			out["log_storage_mode"] = m.Repository.Registry.LogMode()
			out["cleanup_protection"] = "control_linked_events_permanently_protected"
			out["runtime_fields"] = []string{"retention", "notification_settings"}
			out["deployment_fields"] = []string{"credentials", "log_capacity", "connection_limits", "snapshot_key"}
			respond(w, 200, "ok", "", out)
		}},
		{"GET /admin/v1/settings/retention", SystemRead, func(w http.ResponseWriter, r *http.Request) {
			s, e := m.Repository.Retention(r.Context())
			if e != nil {
				operationError(w, e)
				return
			}
			respond(w, 200, "ok", "", s)
		}},
		{"PUT /admin/v1/settings/retention", RetentionWrite, func(w http.ResponseWriter, r *http.Request) {
			var s operations.Retention
			if e := logquery.DecodeRequest(r.Body, &s); e != nil {
				logFailure(w, e)
				return
			}
			actor, _, _ := Identity(r.Context())
			s, e := m.Repository.SaveRetention(r.Context(), s, actor, time.Now().UTC())
			if e != nil {
				operationError(w, e)
				return
			}
			respond(w, 200, "ok", "", s)
		}},
		{"POST /admin/v1/maintenance/cleanup/preview", Cleanup, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Source storage.LogSource `json:"source"`
			}
			if e := logquery.DecodeRequest(r.Body, &body); e != nil {
				logFailure(w, e)
				return
			}
			actor, _, _ := Identity(r.Context())
			p, e := m.Preview(r.Context(), body.Source, actor, time.Now().UTC())
			if e != nil {
				operationError(w, e)
				return
			}
			respond(w, 200, "ok", "", p)
		}},
		{"POST /admin/v1/maintenance/cleanup", Cleanup, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				operations.CleanupRequest
				IdempotencyKey string `json:"idempotency_key"`
			}
			if e := logquery.DecodeRequest(r.Body, &body); e != nil {
				logFailure(w, e)
				return
			}
			actor, _, _ := Identity(r.Context())
			j, e := m.Accept(r.Context(), actor, body.IdempotencyKey, body.CleanupRequest, time.Now().UTC())
			if e != nil {
				operationError(w, e)
				return
			}
			w.Header().Set("Location", "/admin/v1/maintenance/cleanup/"+j.ID)
			respond(w, 202, "accepted", "", j)
		}},
		{"GET /admin/v1/maintenance/cleanup/{id}", Cleanup, func(w http.ResponseWriter, r *http.Request) {
			actor, _, _ := Identity(r.Context())
			j, e := m.Repository.Job(r.Context(), r.PathValue("id"), actor, "cleanup")
			if e != nil {
				operationError(w, e)
				return
			}
			respond(w, 200, "ok", "", j)
		}},
	}
	for _, route := range routes {
		h, e := auth.Protect(route.action, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			route.fn(w, r)
		}))
		if e != nil {
			return e
		}
		mux.Handle(route.pattern, h)
	}
	return nil
}
