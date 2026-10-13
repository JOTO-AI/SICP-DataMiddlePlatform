package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type OperationsViews struct {
	Traces     observability.TaskDiagnostics
	Repository operations.Repository
	Control    interface {
		RecentTasks(context.Context) (control.TaskOverview, error)
	}
	Tokens    *operations.Tokens
	Correlate func(string) string
	Active    interface {
		Read(context.Context) (control.ActiveMetadata, bool, error)
	}
}

func operationError(w http.ResponseWriter, err error) {
	status, code := 503, "unavailable"
	switch {
	case errors.Is(err, operations.ErrArgument):
		status, code = 400, "invalid_argument"
	case errors.Is(err, operations.ErrConflict):
		status, code = 409, "revision_conflict"
	case errors.Is(err, operations.ErrMissing):
		status, code = 404, "not_found"
	case errors.Is(err, operations.ErrLimit):
		status, code = 429, "resource_exhausted"
	case errors.Is(err, operations.ErrForbidden):
		status, code = 403, "forbidden"
	}
	respond(w, status, code, "Management operation failed", nil)
}
func pageInput(r *http.Request, tokens *operations.Tokens, scope string, extra ...string) (operations.Token, url.Values, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	actor, _, ok := Identity(r.Context())
	if !ok {
		return operations.Token{}, nil, operations.ErrForbidden
	}
	if len(r.URL.RawQuery) > 8192 {
		return operations.Token{}, nil, operations.ErrArgument
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return operations.Token{}, nil, operations.ErrArgument
	}
	for k, v := range values {
		allowed := k == "cursor" || k == "page_size"
		for _, e := range extra {
			allowed = allowed || k == e
		}
		if !allowed || len(v) != 1 || v[0] == "" {
			return operations.Token{}, nil, operations.ErrArgument
		}
	}
	v := operations.Token{Scope: scope, Subject: actor, Size: 50, At: now, Expires: now.Add(24 * time.Hour)}
	if token := values.Get("cursor"); token != "" {
		v, err = tokens.Decode(token, scope, actor, now)
		if err != nil {
			return v, nil, err
		}
	}
	if size := values.Get("page_size"); size != "" {
		n, e := strconv.Atoi(size)
		if e != nil || n < 1 || n > 200 || strconv.Itoa(n) != size || values.Get("cursor") != "" && n != v.Size {
			return v, nil, operations.ErrArgument
		}
		v.Size = n
	}
	return v, values, nil
}
func (v OperationsViews) Mount(mux *http.ServeMux, auth *Sessions, detail StatusHandler) error {
	register := func(pattern string, h http.Handler) error {
		protected, e := auth.Protect(Status, h)
		if e == nil {
			mux.Handle(pattern, protected)
		}
		return e
	}
	routes := map[string]http.Handler{
		"GET /admin/v1/update-events": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, _, e := pageInput(r, v.Tokens, "update-events")
			if e != nil {
				operationError(w, e)
				return
			}
			before, id := p.At, ""
			if p.Position != "" {
				at, key, ok := strings.Cut(p.Position, "|")
				if !ok || !operations.ValidID(key) {
					operationError(w, operations.ErrArgument)
					return
				}
				before, e = time.Parse(time.RFC3339Nano, at)
				if e != nil {
					operationError(w, operations.ErrArgument)
					return
				}
				id = key
			}
			items, e := v.Repository.Events(r.Context(), before, id, p.Size+1)
			if e != nil {
				operationError(w, e)
				return
			}
			out := operations.EventPage{Items: items}
			if len(items) > p.Size {
				out.Items = items[:p.Size]
				last := out.Items[p.Size-1]
				p.Position = last.AcceptedAt.Format(time.RFC3339Nano) + "|" + last.ID
				token, e := v.Tokens.Encode(p)
				if e != nil {
					operationError(w, e)
					return
				}
				out.NextCursor = &token
			}
			respond(w, 200, "ok", "", out)
		}),
		"GET /admin/v1/update-events/{id}": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" || !operations.ValidID(r.PathValue("id")) {
				operationError(w, operations.ErrArgument)
				return
			}
			r.SetPathValue("task_id", r.PathValue("id"))
			detail.ServeHTTP(w, r)
		}),
		"GET /admin/v1/update-events/{id}/operations": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parent := r.URL.Query().Get("parent_operation_id")
			p, _, e := pageInput(r, v.Tokens, "operations/"+r.PathValue("id")+"/"+parent, "parent_operation_id")
			if e != nil {
				operationError(w, e)
				return
			}
			items, e := v.Repository.Operations(r.Context(), r.PathValue("id"), parent, p.Position, p.Size+1)
			if e != nil {
				operationError(w, e)
				return
			}
			if strings.HasPrefix(parent, "trace:") {
				traces, err := v.Traces.BuildOperations(r.Context(), r.PathValue("id"), strings.TrimPrefix(parent, "trace:"), p.Position, p.At, p.Size+1)
				if err != nil {
					operationError(w, operations.ErrUnavailable)
					return
				}
				for i := range traces {
					b := &traces[i]
					items = append(items, operations.Operation{ID: b.ID, Parent: &parent, Kind: b.Scope.Stage, State: b.State, Evidence: "platform_log", StartedAt: b.StartedAt, FinishedAt: b.FinishedAt, Build: b})
				}
			}
			var next *string
			if len(items) > p.Size {
				items = items[:p.Size]
				p.Position = items[p.Size-1].ID
				token, e := v.Tokens.Encode(p)
				if e != nil {
					operationError(w, e)
					return
				}
				next = &token
			}
			respond(w, 200, "ok", "", map[string]any{"items": items, "next_cursor": next, "parent_operation_id": parent})
		}),
		"GET /admin/v1/sources": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			items, e := v.Repository.Sources(r.Context(), v.Correlate)
			if e != nil {
				operationError(w, e)
				return
			}
			respond(w, 200, "ok", "", map[string]any{"items": items})
		}),
		"GET /admin/v1/overview": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			if v.Control == nil {
				operationError(w, operations.ErrUnavailable)
				return
			}
			tasks, e := v.Control.RecentTasks(ctx)
			if e != nil {
				operationError(w, operations.ErrUnavailable)
				return
			}
			slots, e := v.Repository.Slots(ctx)
			if e != nil {
				operationError(w, e)
				return
			}
			actions, e := v.Repository.ActionReasons(ctx)
			if e != nil {
				operationError(w, e)
				return
			}
			out := operations.Overview{ObservedAt: time.Now().UTC(), Tasks: tasks, Slots: slots, ActiveState: "unavailable", Actions: actions}
			if v.Active != nil {
				if active, found, e := v.Active.Read(ctx); e == nil {
					out.ActiveState = "empty"
					if found && active.Validate() == nil {
						out.Active = &active
						out.ActiveState = "slot_unavailable"
						for _, slot := range slots {
							if slot.Instance == string(active.Position.Position.Instance) && int(slot.Slot) == active.Position.Position.Slot && slot.Generation == active.Position.Generation && slot.BundleID != nil && *slot.BundleID == active.Position.BundleID && slot.State == "ready" && slot.AdmissionOpen {
								out.ActiveState = "available"
							}
						}
					} else if found {
						out.ActiveState = "unavailable"
					}
				}
			}
			if out.ActiveState != "available" && (tasks.Initialized || out.ActiveState != "empty") {
				for action, reason := range out.Actions {
					if reason == "available_recheck_on_submit" {
						out.Actions[action] = "active_" + out.ActiveState
					}
				}
			}
			respond(w, 200, "ok", "", out)
		}),
	}
	for pattern, h := range routes {
		if e := register(pattern, h); e != nil {
			return e
		}
	}
	return nil
}
