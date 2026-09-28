package admin

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type historyStore struct {
	calls int
	err   error
}

func (s *historyStore) RecentTasks(context.Context) (control.TaskOverview, error) {
	s.calls++
	return control.TaskOverview{Items: []control.TaskSummary{{ID: "automatic-task", Mode: "initialize", Status: "accepted", Operator: "scheduler"}}, IntervalSeconds: 7200}, s.err
}

func TestTaskHistoryRequiresSessionAndDoesNotExposeStoreErrors(t *testing.T) {
	sessions, err := NewSessions("admin", "synthetic-admin-password", "query-key", false)
	if err != nil {
		t.Fatal(err)
	}
	store := &historyStore{}
	h := sessions.Require(TaskListHandler{Store: store})
	r := httptest.NewRequest("GET", "/admin/v1/tasks/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || store.calls != 0 {
		t.Fatal("history leaked without login")
	}
	cookie, _ := loginSession(t, sessions)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "automatic-task") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	store.err = errors.New("private-database-detail")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private-database-detail") || strings.Contains(w.Body.String(), "automatic-task") {
		t.Fatal("store failure leaked partial history", w.Body.String())
	}
}
