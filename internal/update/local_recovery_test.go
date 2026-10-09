package update_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

func (w *changedWorld) ResumeShift(_ context.Context, e control.Publication) (update.ShiftResult, error) {
	err := w.step("resume-drain")
	return update.ShiftResult{Event: e, Drained: err == nil}, err
}

type localState struct {
	saved update.ChangeResult
	fail  error
	reads int
}

func (s *localState) LoadLocal(context.Context, control.Task) (update.ChangeResult, error) {
	s.reads++
	return s.saved, s.fail
}

type recoveredTarget struct {
	calls int
	fail  error
}

func (s *recoveredTarget) Validate(context.Context, candidate.Manifest) error {
	s.calls++
	return s.fail
}
func (s *recoveredTarget) Open(context.Context, candidate.Manifest, candidate.TableVersion) (candidate.Rows, error) {
	return nil, errors.New("not needed by orchestration fixture")
}

func TestT14_07ResumeAfterEachLocalStageKeepsOriginalRoles(t *testing.T) {
	for _, stage := range []string{"build-first", "ready-first", "old-readable", "drain", "build-second", "ready-second"} {
		t.Run(stage, func(t *testing.T) {
			c, w := changedFixture(t)
			w.fail = stage
			saved, err := c.Run(t.Context(), w.active)
			if err == nil {
				t.Fatal("failure not injected")
			}
			w.fail = ""
			w.trace = nil
			out, err := c.Resume(t.Context(), saved)
			if err != nil || out.Copies[1].Target.Position.Instance != saved.Prepared.Plan.Second {
				t.Fatal(out, err, w.trace)
			}
			for _, step := range w.trace {
				if step == "acquire-compare" {
					t.Fatal("reacquired during local resume")
				}
				if saved.Shift.Event.ID != "" && (step == "build-first" || step == "old-readable" || step == "shift") {
					t.Fatal("rebuilt or shifted serving first instance", w.trace)
				}
			}
		})
	}
}

func TestT14_07ResumeRechecksDrainAndRejectsUnknownPublication(t *testing.T) {
	for _, failure := range []string{"drain", "pending", "wrong_role", "old_evidence", "publish"} {
		t.Run(failure, func(t *testing.T) {
			c, w := changedFixture(t)
			w.fail = "build-second"
			saved, _ := c.Run(t.Context(), w.active)
			w.fail = ""
			w.trace = nil
			switch failure {
			case "drain":
				w.fail = "resume-drain"
			case "pending":
				saved.Shift.Event.Status = "pending"
			case "wrong_role":
				saved.Copies[0].Target.Position.Instance = saved.Prepared.Plan.Second
			case "old_evidence":
				saved.Copies[0].Evidence.Target.Generation++
			case "publish":
				saved.Event.ID = "pending-publish"
			}
			if _, err := c.Resume(t.Context(), saved); err == nil {
				t.Fatal("unsafe resume")
			}
			for _, step := range w.trace {
				if strings.HasPrefix(step, "build-") {
					t.Fatal("wrote before recovery gate", w.trace)
				}
			}
		})
	}
}

func TestT14_07And10LocalRecoveryReopensActualFourteenSourceSeal(t *testing.T) {
	for _, damage := range []string{"none", "seal", "target", "roles", "failed"} {
		t.Run(damage, func(t *testing.T) {
			a, requests := acquisition(t, "")
			p := fractionalUpdatePlan()
			lease, err := a.Acquire(t.Context(), p, time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			c, w := changedFixture(t)
			w.prepared.Manifest.Input = lease.Reference()
			w.prepared.Plan = p
			raw, _ := json.Marshal(p)
			roles := control.Document(raw)
			task := control.Task{ID: p.TaskID, Mode: p.Mode, Status: "running", StartedAt: &p.StartedAt, Roles: &roles}
			state := &localState{saved: update.ChangeResult{Prepared: w.prepared}}
			target := &recoveredTarget{}
			switch damage {
			case "seal":
				state.saved.Prepared.Manifest.Input.SHA256 = strings.Repeat("0", 64)
			case "target":
				target.fail = errors.New("target corrupt")
			case "roles":
				roles = "{}"
			case "failed":
				task.Status = "failed"
			}
			r := update.LocalRecovery{Store: state, Files: a.Staging, Targets: target, Changed: c, Zone: time.UTC}
			before := len(*requests)
			_, err = r.Run(t.Context(), task)
			if damage == "none" {
				if err != nil || target.calls != 1 {
					t.Fatal(err)
				}
			} else {
				if err == nil || len(w.trace) != 0 {
					t.Fatal("invalid input reached build", err, w.trace)
				}
			}
			if len(*requests) != before {
				t.Fatal("recovery fetched source")
			}
			if damage == "failed" && state.reads != 0 {
				t.Fatal("failed acquisition reused old checkpoint")
			}
		})
	}
}

type checkpointWorld struct {
	doc    control.Document
	events map[string]*control.Publication
	fail   bool
}

func (w *checkpointWorld) RecoveryCheckpoint(_ context.Context, save *control.Document, _ time.Time) (control.Document, error) {
	if w.fail {
		return "", errors.New("checkpoint failed")
	}
	if save != nil {
		w.doc = *save
	}
	return w.doc, nil
}
func (w *checkpointWorld) RecoveryPublication(_ context.Context, id string) (*control.Publication, error) {
	return w.events[id], nil
}
func TestT14_07JournalUsesDurableOutboxWhenInterruptedBeforeCheckpoint(t *testing.T) {
	c, w := changedFixture(t)
	db := &checkpointWorld{events: map[string]*control.Publication{}}
	j := update.Journal{Store: db, Clock: c.Clock}
	c.Journal = j
	w.fail = "build-second"
	saved, err := c.Run(t.Context(), w.active)
	if err == nil {
		t.Fatal("missing fault")
	}
	saved.Shift.Event.Status = "pending"
	db.events[saved.Shift.Event.ID] = &saved.Shift.Event
	restored, err := j.LoadLocal(t.Context(), control.Task{ID: w.prepared.Plan.TaskID})
	if err != nil || restored.Shift.Event.Status != "pending" || restored.Prepared.Lease != nil {
		t.Fatal(restored, err)
	}
	db.fail = true
	w.fail = ""
	w.trace = nil
	if _, err := c.Run(t.Context(), w.prepared.Plan.Origin); err == nil {
		t.Fatal("ignored journal failure")
	}
	for _, step := range w.trace {
		if strings.HasPrefix(step, "build-") {
			t.Fatal("built before durable checkpoint")
		}
	}
}

func TestT14_18FreshTaskStartsEverySourceAtPageOneWithoutOldPartialPages(t *testing.T) {
	for _, mode := range []string{"incremental", "force_full"} {
		t.Run(mode, func(t *testing.T) {
			p := updatePlan()
			p.Mode = mode
			old, _ := acquisition(t, "dwd_xqjbsjzlb")
			if _, err := old.Acquire(t.Context(), p, time.UTC); err == nil {
				t.Fatal("expected acquisition failure")
			}
			partial, err := old.Staging.Entries(t.Context())
			if err != nil || len(partial) == 0 {
				t.Fatal("missing failed-run partial evidence", err)
			}
			fresh, requests := acquisition(t, "")
			dir, err := staging.OpenDirectory(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = dir.Close() })
			files, err := staging.NewQuotaFiles(dir, 32<<20)
			if err != nil {
				t.Fatal(err)
			}
			p.TaskID = "linked-retry"
			fresh.Owner = p.TaskID
			fresh.Staging, err = staging.NewStore(p.TaskID, files, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := fresh.Acquire(t.Context(), p, time.UTC)
			if err != nil || lease.Reference().RunID != p.TaskID {
				t.Fatal(err)
			}
			first := map[string]bool{}
			for _, r := range *requests {
				if !first[r.name] {
					if r.query.Get("page") != "1" {
						t.Fatal("resumed partial page")
					}
					first[r.name] = true
				}
				if mode == "force_full" && (r.query.Has("startTime") || r.query.Has("endTime")) {
					t.Fatal("full retry used time window")
				}
			}
			if len(first) != 14 {
				t.Fatal("incomplete fresh acquisition")
			}
			after, err := old.Staging.Entries(t.Context())
			if err != nil || len(after) != len(partial) {
				t.Fatal("old partial run changed", err)
			}
		})
	}
}
