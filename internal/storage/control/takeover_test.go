package control_test

import (
	"testing"
	"time"
)

func TestT14_13TakeoverFencesOldResultsWithoutResettingTaskOrSchedule(t *testing.T) {
	r, d, req := taskRepository(t)
	first, err := r.Dispatch(t.Context(), req, "old-worker", req.AcceptedAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := r.Takeover(t.Context(), first.Owner, "recovery-worker")
	if err != nil || next.Generation != first.Owner.Generation+1 || next.TaskID != req.ID {
		t.Fatal(next, err)
	}
	duplicate, err := r.Takeover(t.Context(), first.Owner, "recovery-worker")
	if err != nil || duplicate != next {
		t.Fatal(duplicate, err)
	}
	at := req.AcceptedAt.Add(time.Minute)
	if err = r.WithOwnership(first.Owner).FailTask(t.Context(), "interrupted", &at); err == nil {
		t.Fatal("late old result accepted")
	}
	if _, err = r.Takeover(t.Context(), first.Owner, "third-worker"); err == nil {
		t.Fatal("stale recovery stole owner")
	}
	if err = r.Release(t.Context(), first.Owner); err == nil {
		t.Fatal("old release cleared new owner")
	}
	if d.records[req.IdempotencyKey][7] != req.AcceptedAt || d.records[req.IdempotencyKey][11] != nil || d.revision != 1 || d.owner != next {
		t.Fatal("takeover rewrote task time")
	}
}

func TestT14_13TakeoverFailureRollsBackFenceAndTaskTogether(t *testing.T) {
	for _, failure := range []string{"commit", "task", "candidate"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			first, err := r.Dispatch(t.Context(), req, "old-worker", req.AcceptedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "commit":
				d.failCommit = true
			case "task":
				d.failSQL = "UPDATE control.tasks SET owner_id"
			case "candidate":
				d.failSQL = "UPDATE control.candidates"
			}
			if next, err := r.Takeover(t.Context(), first.Owner, "recovery-worker"); err == nil || next.TaskID != "" || d.owner != first.Owner || d.records[req.IdempotencyKey][12] != "old-worker" {
				t.Fatal(next, err)
			}
		})
	}
}
