package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
)

func TestAutomaticOperationsHaveNoManualHTTPRoute(t *testing.T) {
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("removed action reached handler") })
	mux, err := admin.Routes(administrator(t, admin.ForceFull), map[admin.Action]http.Handler{admin.Incremental: h, admin.ForceFull: h, admin.Rerun: h, admin.Status: h})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"initialize", "sync"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/admin/v1/tasks/"+action, nil))
		if w.Code != 404 && w.Code != 405 {
			t.Fatal("manual route survived", action, w.Code)
		}
	}
}
