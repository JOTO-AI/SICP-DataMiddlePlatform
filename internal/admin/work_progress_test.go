package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
)

type workStatusStore struct{ acquisitionStatusStore }

func (s *workStatusStore) WorkProgress(_ context.Context, id string) (*activity.Snapshot, error) {
	s.calls++
	if id != "task-a" {
		return nil, errors.New("wrong task")
	}
	return &activity.Snapshot{Phase: "building_table", Table: "dwd_bzkscjzlb", StartedAt: time.Now(), HeartbeatAt: time.Now()}, s.err
}

func TestWorkProgressRequiresStatusPermissionAndDoesNotConfirmPublication(t *testing.T) {
	s := &workStatusStore{}
	h := StatusHandler{Store: s, Work: s}
	req := httptest.NewRequest("GET", "/admin/v1/tasks/task-a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 || s.calls != 0 {
		t.Fatal("unauthorized stage access")
	}
	req = req.WithContext(context.WithValue(req.Context(), identityKey{}, identity{operator: "viewer", action: Status}))
	req.SetPathValue("task_id", "task-a")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out struct {
		Data TaskView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || out.Data.WorkProgress == nil || out.Data.WorkProgress.Phase != "building_table" || out.Data.ProgressConfirmed {
		t.Fatal(w.Body.String())
	}
	s.err = errors.New("private database detail")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Body.String())
	}
}
