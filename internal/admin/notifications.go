package admin

import (
	"net/http"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
)

func MountNotifications(mux *http.ServeMux, auth *Sessions, n *operations.Notifier, tokens *operations.Tokens) error {
	if auth == nil || n == nil || tokens == nil {
		return operations.ErrUnavailable
	}
	register := func(pattern string, action Action, fn http.HandlerFunc) error {
		h, e := auth.Protect(action, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" && r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			if e := n.Ensure(r.Context()); e != nil {
				operationError(w, e)
				return
			}
			fn(w, r)
		}))
		if e == nil {
			mux.Handle(pattern, h)
		}
		return e
	}
	routes := []struct {
		pattern string
		action  Action
		fn      http.HandlerFunc
	}{
		{"GET /admin/v1/notifications/decisions", NotificationsRead, func(w http.ResponseWriter, r *http.Request) {
			p, _, e := pageInput(r, tokens, "notification-decisions")
			if e != nil {
				operationError(w, e)
				return
			}
			items, e := n.Repository.Decisions(r.Context(), p.Position, p.Size+1)
			if e != nil {
				operationError(w, e)
				return
			}
			var next *string
			if len(items) > p.Size {
				items = items[:p.Size]
				p.Position = items[p.Size-1].ID
				token, e := tokens.Encode(p)
				if e != nil {
					operationError(w, e)
					return
				}
				next = &token
			}
			respond(w, 200, "ok", "", map[string]any{"items": items, "next_cursor": next})
		}},
		{"GET /admin/v1/settings/notifications", NotificationsRead, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			s, e := n.Repository.NotificationSettings(r.Context())
			if e != nil {
				operationError(w, e)
				return
			}
			state := "unconfigured"
			if n.Channel != nil {
				state = "configured"
			}
			respond(w, 200, "ok", "", map[string]any{"settings": s, "channel_state": state, "channel_diagnostics": n.ChannelDiagnostics()})
		}},
		{"PUT /admin/v1/settings/notifications", NotificationsWrite, func(w http.ResponseWriter, r *http.Request) {
			var s operations.NotificationSettings
			if e := logquery.DecodeRequest(r.Body, &s); e != nil {
				logFailure(w, e)
				return
			}
			subject, _, _ := Identity(r.Context())
			saved, e := n.Repository.SaveNotifications(r.Context(), s, n.Channel != nil, subject, time.Now().UTC())
			if e != nil {
				operationError(w, e)
				return
			}
			respond(w, 200, "ok", "", saved)
		}},
		{"POST /admin/v1/notifications/test", NotificationsWrite, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Revision       int64  `json:"revision,string"`
				IdempotencyKey string `json:"idempotency_key"`
			}
			if e := logquery.DecodeRequest(r.Body, &body); e != nil {
				logFailure(w, e)
				return
			}
			subject, _, _ := Identity(r.Context())
			j, e := n.Repository.AcceptNotificationTest(r.Context(), subject, body.IdempotencyKey, body.Revision, n.Channel != nil, time.Now().UTC())
			if e != nil {
				operationError(w, e)
				return
			}
			w.Header().Set("Location", "/admin/v1/notifications/tests/"+j.ID)
			respond(w, 202, "accepted", "", j)
		}},
		{"GET /admin/v1/notifications/tests/{id}", NotificationsRead, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			subject, _, _ := Identity(r.Context())
			j, e := n.Repository.Job(r.Context(), r.PathValue("id"), subject, "notification_test")
			if e != nil {
				operationError(w, e)
				return
			}
			respond(w, 200, "ok", "", j)
		}},
		{"GET /admin/v1/notifications", NotificationsRead, func(w http.ResponseWriter, r *http.Request) {
			p, _, e := pageInput(r, tokens, "notifications")
			if e != nil {
				operationError(w, e)
				return
			}
			items, e := n.Repository.Deliveries(r.Context(), p.Position, p.Size+1)
			if e != nil {
				operationError(w, e)
				return
			}
			var next *string
			if len(items) > p.Size {
				items = items[:p.Size]
				p.Position = items[p.Size-1].ID
				token, e := tokens.Encode(p)
				if e != nil {
					operationError(w, e)
					return
				}
				next = &token
			}
			respond(w, 200, "ok", "", map[string]any{"items": items, "next_cursor": next})
		}},
		{"GET /admin/v1/notifications/{id}", NotificationsRead, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				operationError(w, operations.ErrArgument)
				return
			}
			d, attempts, e := n.Repository.Delivery(r.Context(), r.PathValue("id"))
			if e != nil {
				operationError(w, e)
				return
			}
			respond(w, 200, "ok", "", map[string]any{"delivery": d, "attempts": attempts})
		}},
	}
	for _, r := range routes {
		if e := register(r.pattern, r.action, r.fn); e != nil {
			return e
		}
	}
	return nil
}
