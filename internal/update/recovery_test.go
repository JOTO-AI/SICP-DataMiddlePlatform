package update_test

import (
	"context"
	"errors"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type recoveryWorld struct {
	state            control.RecoveryState
	active           control.ActiveMetadata
	present, stopped bool
	fail             string
	calls            []string
}

func (w *recoveryWorld) RecoveryState(context.Context) (control.RecoveryState, error) {
	w.calls = append(w.calls, "state")
	if w.fail == "state" {
		return control.RecoveryState{}, errors.New("control unavailable")
	}
	return w.state, nil
}
func (w *recoveryWorld) Read(context.Context) (control.ActiveMetadata, bool, error) {
	w.calls = append(w.calls, "active")
	if w.fail == "active" {
		return control.ActiveMetadata{}, false, errors.New("redis unavailable")
	}
	return w.active, w.present, nil
}
func (w *recoveryWorld) Stopped(context.Context, control.Ownership) (bool, error) {
	w.calls = append(w.calls, "stopped")
	if w.fail == "stopped" {
		return false, errors.New("SQL state unavailable")
	}
	return w.stopped, nil
}
func recoveryFixture() (update.Recovery, *recoveryWorld) {
	p := updatePlan()
	old := p.Origin
	next := old
	next.Sequence++
	next.EventID = "shift"
	next.TaskID = p.TaskID
	next.Position.Position.Instance = p.First
	target, _ := control.ActiveDocument(next)
	expected, _ := control.ActiveDocument(old)
	owner := control.Ownership{TaskID: p.TaskID, OwnerID: "old-worker", Generation: 3}
	task := control.Task{ID: p.TaskID, Mode: p.Mode, Status: "running", StartedAt: &p.StartedAt, OwnerID: &owner.OwnerID, OwnerGeneration: owner.Generation}
	e := control.Publication{ID: next.EventID, TaskID: p.TaskID, Sequence: next.Sequence, Kind: "route_shift", Expected: &expected, Target: target, Status: "pending", CreatedAt: p.StartedAt}
	w := &recoveryWorld{state: control.RecoveryState{Owner: owner, Task: &task, Pending: &e, Confirmed: &old}, active: old, present: true, stopped: true}
	return update.Recovery{Store: w, Active: w, Execution: w}, w
}

func TestT14_05OverdueRestartRecoversBeforeNewScheduling(t *testing.T) {
	r, w := recoveryFixture()
	out, err := r.Inspect(t.Context())
	if err != nil || out.Action != "deliver_publication" || out.State.Pending.ID != "shift" {
		t.Fatal(out, err)
	}
	// Redis already contains the full pending target: confirmation, never a new task.
	w.active, _ = control.DecodeActive(w.state.Pending.Target)
	out, err = r.Inspect(t.Context())
	if err != nil || out.Action != "confirm_publication" {
		t.Fatal(out, err)
	}
	w.state.Pending = nil
	w.state.Confirmed = &w.active
	out, err = r.Inspect(t.Context())
	if err != nil || out.Action != "inspect_local" {
		t.Fatal(out, err)
	}
	w.state.Task = nil
	w.state.Owner = control.Ownership{}
	out, err = r.Inspect(t.Context())
	if err != nil || out.Action != "idle" {
		t.Fatal(out, err)
	}
}

func TestT14_09UncertainLocationOrExecutionRemainsPending(t *testing.T) {
	for _, failure := range []string{"state", "active", "stopped", "missing", "wrong", "running_sql", "wrong_event", "wrong_expected", "missing_confirmed", "orphan_owner"} {
		t.Run(failure, func(t *testing.T) {
			r, w := recoveryFixture()
			w.fail = failure
			switch failure {
			case "missing":
				w.present = false
			case "wrong":
				w.active.Sequence += 5
			case "running_sql":
				w.stopped = false
			case "wrong_event":
				w.state.Pending.ID = "other"
			case "wrong_expected":
				w.state.Pending.Expected = nil
			case "missing_confirmed":
				w.state.Pending = nil
				w.present = false
			case "orphan_owner":
				w.state.Pending = nil
				w.state.Task = nil
			}
			out, err := r.Inspect(t.Context())
			if !errors.Is(err, update.ErrRecoveryPending) || out.Action != "pending" {
				t.Fatal(out, err)
			}
		})
	}
}

func TestT14_11RecoveryDiscoveryDoesNotInventFinishedAt(t *testing.T) {
	r, w := recoveryFixture()
	started := *w.state.Task.StartedAt
	for i := 0; i < 2; i++ {
		out, err := r.Inspect(t.Context())
		if err != nil || out.State.Task.FinishedAt != nil || *out.State.Task.StartedAt != started || out.State.Task.Status != "running" {
			t.Fatal(out, err)
		}
	}
}

func TestT14_05EmptyStartupAndFirstPublicationAreDistinct(t *testing.T) {
	r, w := recoveryFixture()
	w.state.Confirmed = nil
	w.state.Pending.Expected = nil
	w.state.Pending.Kind = "bundle_publish"
	w.present = false
	out, err := r.Inspect(t.Context())
	if err != nil || out.Action != "deliver_publication" {
		t.Fatal(out, err)
	}
	w.state = control.RecoveryState{}
	out, err = r.Inspect(t.Context())
	if err != nil || out.Action != "idle" {
		t.Fatal(out, err)
	}
	w.present = true
	out, err = r.Inspect(t.Context())
	if !errors.Is(err, update.ErrRecoveryPending) || out.Action != "pending" {
		t.Fatal(out, err)
	}
}

func (w *recoveryWorld) Takeover(_ context.Context, old control.Ownership, worker string) (control.Ownership, error) {
	w.calls = append(w.calls, "takeover")
	return control.Ownership{TaskID: old.TaskID, OwnerID: worker, Generation: old.Generation + 1}, nil
}
func TestT14_13ClaimRequiresActualExecutorAndSQLTermination(t *testing.T) {
	r, w := recoveryFixture()
	w.stopped = false
	if _, err := r.Claim(t.Context(), "new-worker"); !errors.Is(err, update.ErrRecoveryPending) {
		t.Fatal(err)
	}
	for _, step := range w.calls {
		if step == "takeover" {
			t.Fatal("claimed before termination")
		}
	}
	w.stopped = true
	owner, err := r.Claim(t.Context(), "new-worker")
	if err != nil || owner.Generation != w.state.Owner.Generation+1 || owner.TaskID != w.state.Task.ID {
		t.Fatal(owner, err)
	}
}

func TestT14_08ShiftRecoveryDoesNotConfirmSourceProgress(t *testing.T) {
	c, w := changedFixture(t)
	w.fail = "build-second"
	saved, err := c.Run(t.Context(), w.active)
	if err == nil {
		t.Fatal("missing interruption")
	}
	w.fail = ""
	w.trace = nil
	f := update.Finisher{Store: w, Delivery: w, Clock: c.Clock}
	event, err := f.Recover(t.Context(), saved.Shift.Event)
	if err != nil || event.Kind != "route_shift" {
		t.Fatal(event, err)
	}
	for _, step := range w.trace {
		if step == "progress-success" || step == "build-first" || step == "build-second" {
			t.Fatal("shift recovery changed data or progress")
		}
	}
}
