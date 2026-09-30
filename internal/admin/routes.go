package admin

import (
	"errors"
	"net/http"
)

func Routes(auth interface {
	Protect(Action, http.Handler) (http.Handler, error)
}, handlers map[Action]http.Handler) (http.Handler, error) {
	if len(handlers) != 3 {
		return nil, errors.New("all management actions must be configured")
	}
	mux := http.NewServeMux()
	for _, route := range []struct {
		action  Action
		pattern string
	}{
		{ForceFull, "POST /admin/v1/tasks/force-full"},
		{ForceFull, "POST /admin/v1/tables/{table}/force-full"},
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
