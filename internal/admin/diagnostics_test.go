package admin

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
)

type failureDiagnostics struct {
	calls int
	fail  bool
}

func (d *failureDiagnostics) FailedAttempts(_ context.Context, id string) ([]observability.FailedAttempt, error) {
	d.calls++
	if d.fail {
		return []observability.FailedAttempt{{Source: "must-not-escape"}}, errors.New("private error")
	}
	if id != "task-a" {
		return nil, errors.New("wrong task")
	}
	status := 500
	return []observability.FailedAttempt{{Source: "dwd_bzkscjzlb", Page: 1115, Attempt: 3, HTTPStatus: &status, Failure: "http"}}, nil
}
func TestDiagnosticsRequireAuthorizationAndFailWithoutHidingTask(t *testing.T) {
	d := &failureDiagnostics{}
	h := StatusHandler{Store: &acquisitionStatusStore{}, Diagnostics: d}
	r := httptest.NewRequest("GET", "/admin/v1/tasks/task-a", nil)
	r.SetPathValue("task_id", "task-a")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || d.calls != 0 {
		t.Fatal("unauthorized logs read")
	}
	r = r.WithContext(context.WithValue(r.Context(), identityKey{}, identity{operator: "viewer", action: Status}))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"page":1115`) {
		t.Fatal("missing authorized diagnostic")
	}
	d.fail = true
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"diagnostics_unavailable":true`) || strings.Contains(w.Body.String(), "private error") || strings.Contains(w.Body.String(), "must-not-escape") {
		t.Fatal("logs failure hid status or exposed partial data")
	}
}
