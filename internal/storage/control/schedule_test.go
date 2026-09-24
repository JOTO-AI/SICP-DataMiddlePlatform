package control_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func finishScheduled(t *testing.T, r control.Repository, d *taskDB, launch control.Launch, status string) {
	t.Helper()
	d.records[launch.Task.IdempotencyKey][3] = status
	if err := r.Release(t.Context(), launch.Owner); err != nil {
		t.Fatal(err)
	}
}
func nextRequest(req control.Request, at time.Time) control.Request {
	req.ID += "-next"
	req.IdempotencyKey += "-next"
	req.AcceptedAt = at
	return req
}

func TestT14_01ManualStartInvalidatesOldTimer(t *testing.T) {
	r, d, req := taskRepository(t)
	first, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
	if err != nil || !first.Started {
		t.Fatal(first, err)
	}
	oldRevision := d.revision
	finishScheduled(t, r, d, first, "success_published")
	clock := testkit.FakeClock{Time: req.AcceptedAt.Add(90 * time.Minute)}
	at := clock.Now()
	manual, err := r.Dispatch(t.Context(), nextRequest(req, at), "worker", at, nil)
	if err != nil || !manual.Started {
		t.Fatal(manual, err)
	}
	timer := nextRequest(nextRequest(req, at), req.AcceptedAt.Add(2*time.Hour))
	if _, err = r.Dispatch(t.Context(), timer, "timer", timer.AcceptedAt, &oldRevision); !errors.Is(err, control.ErrStaleTimer) {
		t.Fatal(err)
	}
	if !d.nextDue.Equal(req.AcceptedAt.Add(210*time.Minute)) || len(d.records) != 2 {
		t.Fatal("old timer changed schedule")
	}
}

func TestT14_02BusyTimerCoalescesAndRebasesOnlyOnActualStart(t *testing.T) {
	r, d, req := taskRepository(t)
	first, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	revision := d.revision
	due := req.AcceptedAt.Add(2 * time.Hour)
	for i := 0; i < 3; i++ {
		out, err := r.Dispatch(t.Context(), nextRequest(req, due), "timer", due, &revision)
		if err != nil || out.Started || !out.Deferred {
			t.Fatal(out, err)
		}
	}
	if len(d.records) != 1 || !d.deferred || d.revision != revision {
		t.Fatal("busy timer queued or reset task")
	}
	writes := 0
	for _, c := range d.calls {
		if c.sql == "UPDATE control.schedule SET deferred=TRUE WHERE singleton=TRUE AND deferred=FALSE" {
			writes++
		}
	}
	if writes != 1 {
		t.Fatal("deferral was not coalesced", writes)
	}
	finishScheduled(t, r, d, first, "success_unchanged")
	at := due.Add(20 * time.Minute)
	out, err := r.Dispatch(t.Context(), nextRequest(req, at), "timer", at, &revision)
	if err != nil || !out.Started || d.deferred || !d.nextDue.Equal(req.AcceptedAt.Add(260*time.Minute)) {
		t.Fatal(out, err)
	}
}

func TestT14_03TerminalOutcomeDoesNotChangeStartSchedule(t *testing.T) {
	for _, status := range []string{"failed", "success_unchanged", "success_published"} {
		t.Run(status, func(t *testing.T) {
			r, d, req := taskRepository(t)
			out, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			finishScheduled(t, r, d, out, status)
			s, err := r.CurrentSchedule(t.Context())
			if err != nil || s.Revision != 1 || !s.LastStartedAt.Equal(req.AcceptedAt) || !s.NextDueAt.Equal(req.AcceptedAt.Add(2*time.Hour)) {
				t.Fatal(s, err)
			}
		})
	}
}

func TestT14_04DuplicateResumeRestartAndRejectionPreserveStartedAt(t *testing.T) {
	r, d, req := taskRepository(t)
	first, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	retry := req
	retry.ID = "unused"
	retry.AcceptedAt = retry.AcceptedAt.Add(time.Hour)
	second, err := r.Dispatch(t.Context(), retry, "other", retry.AcceptedAt, nil)
	if err != nil || second.Started || second.Task.ID != first.Task.ID || !second.Task.StartedAt.Equal(req.AcceptedAt) {
		t.Fatal(second, err)
	}
	restarted := control.Repository{Registry: r.Registry, Database: d}
	if at, err := restarted.Start(t.Context(), first.Owner, retry.AcceptedAt); err != nil || !at.Equal(req.AcceptedAt) {
		t.Fatal(at, err)
	}
	retry.Mode = "force_full"
	if _, err := restarted.Dispatch(t.Context(), retry, "other", retry.AcceptedAt, nil); !errors.Is(err, control.ErrIdempotency) {
		t.Fatal(err)
	}
	if len(d.records) != 1 || d.revision != 1 || !d.nextDue.Equal(req.AcceptedAt.Add(2*time.Hour)) {
		t.Fatal("reset on replay")
	}
}

func TestT14_06ManualAndTimerRaceStartsOnlyOneTask(t *testing.T) {
	r, d, req := taskRepository(t)
	last := req.AcceptedAt
	due := last.Add(2 * time.Hour)
	d.lastStarted = &last
	d.nextDue = &due
	d.revision = 1
	revision := int64(1)
	manual := nextRequest(req, due)
	timer := nextRequest(manual, due)
	var wg sync.WaitGroup
	results := make(chan control.Launch, 2)
	errs := make(chan error, 2)
	for i, request := range []control.Request{manual, timer} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var rev *int64
			if i == 1 {
				rev = &revision
			}
			out, err := r.Dispatch(t.Context(), request, "worker", due, rev)
			results <- out
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	starts := 0
	for out := range results {
		if out.Started {
			starts++
		}
	}
	for err := range errs {
		if err != nil && !errors.Is(err, control.ErrStaleTimer) {
			t.Fatal(err)
		}
	}
	if starts != 1 || len(d.records) != 1 || d.revision != 2 || !d.nextDue.Equal(due.Add(2*time.Hour)) {
		t.Fatal("race admitted multiple tasks", starts)
	}
}

func TestT14_04DispatchRollbackAndAdmissionBarriers(t *testing.T) {
	for _, failure := range []string{"commit", "owner", "start", "schedule", "pending", "cooldown", "unfinished"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			switch failure {
			case "commit":
				d.failCommit = true
			case "owner":
				d.failSQL = "UPDATE control.execution"
			case "start":
				d.failSQL = "SET started_at"
			case "schedule":
				d.failSQL = "UPDATE control.schedule"
			case "pending":
				d.pending = true
			case "cooldown":
				at := req.AcceptedAt.Add(time.Hour)
				d.cooldown = &at
			case "unfinished":
				if _, err := r.Accept(t.Context(), req); err != nil {
					t.Fatal(err)
				}
				req = nextRequest(req, req.AcceptedAt)
			}
			out, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
			blocked := failure == "pending" || failure == "cooldown" || failure == "unfinished"
			if blocked {
				if err != nil || !out.Deferred {
					t.Fatal(out, err)
				}
			} else if err == nil {
				t.Fatal("failure ignored")
			}
			expected := 0
			if failure == "unfinished" {
				expected = 1
			}
			if out.Started || len(d.records) != expected || d.owner.TaskID != "" || d.revision != 0 || d.lastStarted != nil {
				t.Fatal("partial start escaped", out)
			}
		})
	}
}
