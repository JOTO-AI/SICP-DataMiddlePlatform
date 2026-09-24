package control_test

import (
	"reflect"
	"testing"
	"time"
)

func TestT14_10And18FailedRerunCreatesNewLinkedHistoryWithoutResettingSchedule(t *testing.T) {
	for _, mode := range []string{"incremental", "force_full"} {
		t.Run(mode, func(t *testing.T) {
			r, d, req := taskRepository(t)
			req.Mode = mode
			launch, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			writer := r.WithOwnership(launch.Owner)
			if err = writer.FailTask(t.Context(), "acquisition_failed", nil); err != nil {
				t.Fatal(err)
			}
			old := append([]any(nil), d.records[req.IdempotencyKey]...)
			old[12] = nil // Ownership is released atomically with the linked acceptance.
			retry := nextRequest(req, req.AcceptedAt.Add(time.Hour))
			retry.PredecessorID = &req.ID
			out, err := writer.RestartFailed(t.Context(), retry)
			if err != nil || !out.Created || out.Task.ID == req.ID || out.Task.StartedAt != nil || out.Task.Roles != nil || *out.Task.PredecessorID != req.ID {
				t.Fatal(out, err)
			}
			again, err := writer.RestartFailed(t.Context(), retry)
			if err != nil || again.Created || again.Task.ID != out.Task.ID {
				t.Fatal(again, err)
			}
			if !reflect.DeepEqual(old, d.records[req.IdempotencyKey]) || d.records[req.IdempotencyKey][11] != nil || d.revision != 1 || len(d.records) != 2 {
				t.Fatal("old history or schedule changed")
			}
			another := nextRequest(retry, retry.AcceptedAt)
			if _, err = writer.RestartFailed(t.Context(), another); err == nil || len(d.records) != 2 {
				t.Fatal("queued multiple successors")
			}
		})
	}
}

func TestT14_10FailureAndRerunRejectStaleOwnerPendingAndFailedCommit(t *testing.T) {
	for _, failure := range []string{"owner", "pending", "commit", "time"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			launch, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			at := req.AcceptedAt.Add(time.Minute)
			switch failure {
			case "owner":
				launch.Owner.Generation++
			case "pending":
				d.pending = true
			case "commit":
				d.failCommit = true
			case "time":
				at = req.AcceptedAt.Add(-time.Minute)
			}
			if err = r.WithOwnership(launch.Owner).FailTask(t.Context(), "acquisition_failed", &at); err == nil || d.records[req.IdempotencyKey][3] != "running" || d.records[req.IdempotencyKey][11] != nil {
				t.Fatal("unsafe failure confirmation", err)
			}
		})
	}
}

func TestT14_11ObservedFailureTimeIsPreservedOnRecoveryReplay(t *testing.T) {
	r, d, req := taskRepository(t)
	launch, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	writer := r.WithOwnership(launch.Owner)
	at := req.AcceptedAt.Add(time.Minute)
	if err = writer.FailTask(t.Context(), "acquisition_failed", &at); err != nil {
		t.Fatal(err)
	}
	if err = writer.FailTask(t.Context(), "interrupted", nil); err != nil || d.records[req.IdempotencyKey][11] != at {
		t.Fatal("rewrote observed termination", err)
	}
}
