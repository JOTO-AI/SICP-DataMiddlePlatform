package admin

import (
	"errors"
	"net/http"
)

func Routes(auth *Auth, handlers map[Action]http.Handler) (http.Handler, error) {
	if len(handlers) != 5 {
		return nil, errors.New("all management actions must be configured")
	}
	mux := http.NewServeMux()
	for _, route := range []struct {
		action  Action
		pattern string
	}{
		{Initialize, "POST /admin/v1/tasks/initialize"},
		{Sync, "POST /admin/v1/tasks/sync"},
		{ForceFull, "POST /admin/v1/tasks/force-full"},
		{Rerun, "POST /admin/v1/tasks/{task_id}/rerun"},
		{Status, "GET /admin/v1/tasks/{task_id}"},
	} {
		handler, err := auth.Protect(route.action, handlers[route.action])
		if err != nil {
			return nil, err
		}
		mux.Handle(route.pattern, handler)
	}
	return mux, nil
}
