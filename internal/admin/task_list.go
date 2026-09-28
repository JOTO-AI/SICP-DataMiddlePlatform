package admin

import (
	"context"
	"net/http"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type TaskListHandler struct {
	Store interface {
		RecentTasks(context.Context) (control.TaskOverview, error)
	}
}

// Authentication is applied by the console router, exactly as for health.
func (h TaskListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		respond(w, 503, "unavailable", "Task history unavailable", nil)
		return
	}
	out, err := h.Store.RecentTasks(r.Context())
	if err != nil {
		respond(w, 503, "unavailable", "Task history unavailable", nil)
		return
	}
	respond(w, 200, "ok", "OK", out)
}
