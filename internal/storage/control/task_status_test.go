package control_test

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestT15_07StatusSeparatesTimesUnknownEndAndWhitelistedSummary(t *testing.T) {
	r, d, req := taskRepository(t)
	if _, err := r.Accept(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	row := d.records[req.IdempotencyKey]
	start := req.AcceptedAt.Add(time.Minute)
	ready := start.Add(10 * time.Minute)
	published := start.Add(13 * time.Minute)
	checked := start.Add(12 * time.Minute)
	row[3] = "published_pending_progress"
	row[7] = start
	row[8] = ready
	row[9] = published
	row[10] = checked
	row[15] = `{"failure":"corrupt_seal","dsn":"synthetic-secret","source_person":"private-person"}`
	auth, err := admin.NewAuth("query-synthetic", []admin.Credential{{Operator: "viewer", Token: "status-synthetic-secret", Actions: []admin.Action{admin.Status}}})
	if err != nil {
		t.Fatal(err)
	}
	h, err := auth.Protect(admin.Status, admin.StatusHandler{Store: r})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/admin/v1/tasks/"+req.ID, nil)
	request.SetPathValue("task_id", req.ID)
	request.Header.Set("Authorization", "Bearer status-synthetic-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	var result struct {
		Data admin.TaskView `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	view := result.Data
	if w.Code != 200 || view.FinishedAt != nil || view.ProgressConfirmed || view.Progress != nil || view.Failure != "corrupt_seal" || view.Counts != nil || *view.StartedAt != start || *view.ReadyAt != ready || *view.PublishedAt != published || *view.CheckedAt != checked || view.AcceptedAt != req.AcceptedAt {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, secret := range []string{"synthetic-secret", "private-person", req.IdempotencyKey, "owner_generation", "roles"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("status leaked internal payload", secret)
		}
	}
	if !strings.Contains(w.Body.String(), `"finished_at":null`) {
		t.Fatal("unknown end not explicit null")
	}
}

func TestT15_07HistoricalProgressUsesTaskPlanInsteadOfLatestGlobalProgress(t *testing.T) {
	r, d, p, i := progressFixture(t)
	event := appliedEvent(t, r, p, i)
	at := p.StartedAt.Add(30 * time.Minute)
	confirmed, err := r.ConfirmPublication(t.Context(), event, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.CompletePublished(t.Context(), confirmed, at); err != nil {
		t.Fatal(err)
	}
	for name, progress := range d.progress {
		progress.TaskID = "later-task"
		progress.ConfirmedUntil = progress.ConfirmedUntil.Add(2 * time.Hour)
		d.progress[name] = progress
	}
	view, err := r.TaskStatus(t.Context(), d.task.ID)
	if err != nil || view.Progress == nil || !reflect.DeepEqual(view.Progress.Ends, p.Ends) || view.Progress.TaskID != d.task.ID {
		t.Fatal(view, err)
	}
	d.base.failCommit = true
	if view, err = r.TaskStatus(t.Context(), d.task.ID); err == nil || view.Task.ID != "" {
		t.Fatal("failed status transaction returned data")
	}
}

func TestT15_08HTTPRerunCreatesLinkedTaskAndPreservesFailedHistory(t *testing.T) {
	r, d, req := taskRepository(t)
	d.initialized = true
	launch, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.WithOwnership(launch.Owner).FailTask(t.Context(), "acquisition_failed", nil); err != nil {
		t.Fatal(err)
	}
	if err = r.Release(t.Context(), launch.Owner); err != nil {
		t.Fatal(err)
	}
	old := append([]any(nil), d.records[req.IdempotencyKey]...)
	h := management(t, r, req.AcceptedAt.Add(time.Hour), admin.Rerun)
	request := managementRequest(t.Context())
	request.SetPathValue("task_id", req.ID)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	out := receipt(t, w)
	if w.Code != 202 || !out.Created || out.TaskID == req.ID || len(d.records) != 2 || d.records["request-key"][5] != req.ID || d.records["request-key"][2] != req.Mode || !reflect.DeepEqual(old, d.records[req.IdempotencyKey]) {
		t.Fatal(w.Code, w.Body.String())
	}
	again := managementRequest(t.Context())
	again.SetPathValue("task_id", req.ID)
	replayed := httptest.NewRecorder()
	h.ServeHTTP(replayed, again)
	if replayed.Code != 202 || receipt(t, replayed).TaskID != out.TaskID || receipt(t, replayed).Created || len(d.records) != 2 {
		t.Fatal(replayed.Code, replayed.Body.String())
	}
}

func TestHTTPManualInitializationRetryWorksBeforeFirstPublication(t *testing.T) {
	r, d, old := taskRepository(t)
	old.Mode = "initialize"
	if _, err := r.Admit(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	d.records[old.IdempotencyKey][3] = "failed"
	d.records[old.IdempotencyKey][15] = `{"failure":"acquisition_failed"}`
	// Manual retry need not wait for the next worker restart or startup retry timer.
	h := management(t, r, old.AcceptedAt.Add(time.Second), admin.Rerun)
	var newID string
	for attempt := range 2 {
		request := managementRequest(t.Context())
		request.SetPathValue("task_id", old.ID)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request)
		out := receipt(t, w)
		if w.Code != 202 || out.Created != (attempt == 0) {
			t.Fatal(w.Code, w.Body.String())
		}
		if attempt == 0 {
			newID = out.TaskID
		} else if out.TaskID != newID {
			t.Fatal("retry duplicated initialization")
		}
	}
	child := d.records["request-key"]
	if d.initialized || len(d.records) != 2 || child[2] != "initialize" || child[5] != old.ID || d.records[old.IdempotencyKey][3] != "failed" {
		t.Fatal("manual initialization retry changed mode or history", child)
	}
	launch, err := r.StartAccepted(t.Context(), newID, "worker", old.AcceptedAt.Add(2*time.Second))
	if err != nil || !launch.Started || launch.Task.Mode != "initialize" {
		t.Fatal("manual retry could not start", err)
	}
}

func TestT15_08RerunRejectsNonFailureMissingTaskAndCommitFailure(t *testing.T) {
	for _, failure := range []string{"not_failed", "missing", "commit", "owned"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			d.initialized = true
			launch, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			if failure != "not_failed" {
				if err = r.WithOwnership(launch.Owner).FailTask(t.Context(), "interrupted", nil); err != nil {
					t.Fatal(err)
				}
			}
			if failure != "owned" {
				if err = r.Release(t.Context(), launch.Owner); err != nil {
					t.Fatal(err)
				}
			}
			retry := nextRequest(req, req.AcceptedAt.Add(time.Hour))
			retry.Mode = ""
			retry.PredecessorID = &req.ID
			wanted := control.ErrNotFailed
			switch failure {
			case "missing":
				missing := "absent"
				retry.PredecessorID = &missing
				wanted = control.ErrMissing
			case "commit":
				d.failCommit = true
			case "owned":
				wanted = control.ErrBusy
			}
			out, err := r.RerunTask(t.Context(), retry)
			if err == nil || out.Task.ID != "" || len(d.records) != 1 {
				t.Fatal(out, err)
			}
			if failure != "commit" && !errors.Is(err, wanted) {
				t.Fatal(err)
			}
		})
	}
}
