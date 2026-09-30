package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type acquisitionStatusStore struct {
	progress *control.AcquisitionProgress
	err      error
	calls    int
}

func (s *acquisitionStatusStore) TaskStatus(context.Context, string) (control.TaskStatus, error) {
	return control.TaskStatus{Task: control.Task{ID: "task-a", Mode: "initialize", Status: "running", Summary: `{"private":"secret"}`}}, nil
}
func (s *acquisitionStatusStore) AcquisitionProgress(_ context.Context, id string) (*control.AcquisitionProgress, error) {
	s.calls++
	if id != "task-a" {
		return nil, errors.New("wrong task")
	}
	return s.progress, s.err
}

func TestTaskStatusExposesPageCountsOnlyToAuthorizedViewers(t *testing.T) {
	total := int64(1440)
	s := &acquisitionStatusStore{progress: &control.AcquisitionProgress{Tables: []control.TableAcquisitionProgress{{Table: "dwd_bzkscjzlb", Status: "acquiring", CompletedPages: 720, TotalPages: &total}}}}
	h := StatusHandler{Store: s, Acquisition: s}
	req := httptest.NewRequest("GET", "/admin/v1/tasks/task-a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 || s.calls != 0 {
		t.Fatal("unauthorized progress access")
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
	if w.Code != 200 || out.Data.Acquisition == nil || out.Data.Acquisition.Tables[0].CompletedPages != 720 || *out.Data.Acquisition.Tables[0].TotalPages != 1440 || out.Data.ProgressConfirmed || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	s.err = errors.New("private database detail")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "720") {
		t.Fatal(w.Body.String())
	}
}
