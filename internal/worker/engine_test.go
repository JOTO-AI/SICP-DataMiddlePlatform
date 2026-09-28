package worker_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/worker"
)

type world struct {
	trace                                           []string
	decision                                        update.RecoveryDecision
	inspectError, claimError, startError, workError error
	schedule                                        control.Schedule
	launch                                          control.Launch
	resumed                                         bool
	event                                           *control.Publication
}

func (w *world) Inspect(context.Context) (update.RecoveryDecision, error) {
	w.trace = append(w.trace, "inspect")
	return w.decision, w.inspectError
}
func (w *world) Claim(context.Context, string) (control.Ownership, error) {
	w.trace = append(w.trace, "claim")
	o := w.decision.State.Owner
	o.OwnerID = "current"
	o.Generation++
	return o, w.claimError
}
func (w *world) Bootstrap(_ context.Context, r control.Request) (control.Acceptance, error) {
	w.trace = append(w.trace, "bootstrap")
	if r.Mode != "initialize" || r.RequestedBy != "scheduler" || r.IdempotencyKey != "system-initialize-v1" {
		return control.Acceptance{}, errors.New("incorrect bootstrap request")
	}
	return control.Acceptance{}, nil
}
func (w *world) CurrentSchedule(context.Context) (control.Schedule, error) {
	w.trace = append(w.trace, "schedule")
	return w.schedule, nil
}
func (w *world) StartAccepted(_ context.Context, id, process string, at time.Time) (control.Launch, error) {
	w.trace = append(w.trace, "start")
	return w.launch, w.startError
}
func (w *world) Dispatch(_ context.Context, r control.Request, process string, at time.Time, revision *int64) (control.Launch, error) {
	w.trace = append(w.trace, "dispatch")
	if revision == nil || *revision != w.schedule.Revision || r.Mode != "incremental" || r.RequestedBy != "scheduler" || !r.AcceptedAt.Equal(at) {
		return control.Launch{}, errors.New("incorrect timer request")
	}
	return w.launch, nil
}
func (w *world) Release(_ context.Context, o control.Ownership) error {
	w.trace = append(w.trace, "release")
	return nil
}
func (w *world) Run(_ context.Context, t control.Task, o control.Ownership, resume bool, event *control.Publication) error {
	w.trace = append(w.trace, "work")
	w.resumed = resume
	w.event = event
	if o.OwnerID != "current" {
		return errors.New("old owner passed to work")
	}
	return w.workError
}
func engine(w *world) *worker.Engine {
	return &worker.Engine{Store: w, Recovery: w, Work: w, Clock: testkit.FakeClock{Time: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)}, Random: testkit.FixedRandom(4), Process: "current"}
}
func TestT15_10PendingRecoveryAlwaysPrecedesAndExcludesScheduling(t *testing.T) {
	for _, failure := range []string{"none", "inspection", "claim", "work"} {
		t.Run(failure, func(t *testing.T) {
			task := control.Task{ID: "old-task", Status: "running"}
			event := control.Publication{ID: "pending", Kind: "bundle_publish", Status: "pending"}
			w := &world{decision: update.RecoveryDecision{Action: "confirm_publication", State: control.RecoveryState{Task: &task, Pending: &event, Owner: control.Ownership{TaskID: task.ID, OwnerID: "old", Generation: 2}}}}
			bad := errors.New("injected")
			switch failure {
			case "inspection":
				w.inspectError = bad
			case "claim":
				w.claimError = bad
			case "work":
				w.workError = bad
			}
			e := engine(w)
			err := e.Cycle(t.Context())
			if (failure != "none") != (err != nil) {
				t.Fatal(err)
			}
			want := []string{"inspect"}
			if failure != "inspection" {
				want = append(want, "claim")
				if failure != "claim" {
					want = append(want, "work")
					if !w.resumed || w.event != &event {
						t.Fatal("pending evidence lost")
					}
				}
			}
			if !reflect.DeepEqual(w.trace, want) || e.Ready(t.Context()) {
				t.Fatal(w.trace)
			}
		})
	}
}
func TestT15_10AcceptedTaskStartsThroughPersistentGate(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "cooldown"}[blocked], func(t *testing.T) {
			task := control.Task{ID: "accepted", Status: "accepted"}
			w := &world{decision: update.RecoveryDecision{Action: "inspect_local", State: control.RecoveryState{Task: &task}}, launch: control.Launch{Started: true, Task: task, Owner: control.Ownership{TaskID: task.ID, OwnerID: "current", Generation: 1}}}
			if blocked {
				w.startError = control.ErrBusy
			}
			err := engine(w).Cycle(t.Context())
			want := []string{"inspect", "start"}
			if !blocked {
				want = append(want, "work")
			}
			if !reflect.DeepEqual(w.trace, want) || w.resumed || blocked != (err != nil) {
				t.Fatal(w.trace, err)
			}
		})
	}
}
func TestT15_10OnlyIdleConfirmedEnvironmentCanDispatchDueTimer(t *testing.T) {
	at := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"due", "uninitialized", "future", "cooldown", "unresolved"} {
		t.Run(scenario, func(t *testing.T) {
			w := &world{decision: update.RecoveryDecision{Action: "idle", State: control.RecoveryState{Confirmed: &control.ActiveMetadata{}}}, schedule: control.Schedule{Revision: 8, NextDueAt: &at}, launch: control.Launch{Started: true, Owner: control.Ownership{OwnerID: "current"}}}
			future := at.Add(2 * time.Hour)
			switch scenario {
			case "uninitialized":
				w.decision.State.Confirmed = nil
			case "future":
				w.schedule.NextDueAt = &future
			case "cooldown":
				w.schedule.CooldownUntil = &future
			case "unresolved":
				w.decision.Action = "pending"
			}
			e := engine(w)
			err := e.Cycle(t.Context())
			want := []string{"inspect"}
			if scenario != "unresolved" {
				want = append(want, "schedule")
			}
			if scenario == "uninitialized" {
				want = append(want, "bootstrap")
			}
			if scenario == "due" {
				want = append(want, "dispatch", "work")
			}
			if !reflect.DeepEqual(w.trace, want) {
				t.Fatal(w.trace, err)
			}
			if scenario == "unresolved" && err == nil {
				t.Fatal("unresolved state admitted")
			}
		})
	}
}
func TestT15_10RecoveredTerminalTaskFinishesDurableEventsBeforeRelease(t *testing.T) {
	for _, status := range []string{"failed", "success_published", "success_unchanged"} {
		t.Run(status, func(t *testing.T) {
			task := control.Task{ID: "terminal", Status: status}
			w := &world{decision: update.RecoveryDecision{Action: "inspect_local", State: control.RecoveryState{Task: &task, Owner: control.Ownership{TaskID: task.ID, OwnerID: "current", Generation: 3}}}}
			if e := engine(w).Cycle(t.Context()); e != nil || !w.resumed || !reflect.DeepEqual(w.trace, []string{"inspect", "work"}) {
				t.Fatal(w.trace, e)
			}
		})
	}
}
