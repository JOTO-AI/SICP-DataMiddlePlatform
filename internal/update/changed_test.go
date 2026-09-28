package update_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type changedWorld struct {
	t            *testing.T
	trace        []string
	fail         string
	prepared     update.PreparedUpdate
	old          update.Copy
	active       control.ActiveMetadata
	progress     control.ProgressPlan
	publishCalls int
}

func (w *changedWorld) step(s string) error {
	w.trace = append(w.trace, s)
	if w.fail == s {
		return errors.New("synthetic changed-round failure")
	}
	return nil
}
func (w *changedWorld) Prepare(context.Context, control.ActiveMetadata) (update.PreparedUpdate, error) {
	return w.prepared, w.step("acquire-compare")
}
func (w *changedWorld) PlanCopies(context.Context, update.PreparedUpdate) ([2]source.Target, update.Copy, error) {
	p := w.prepared.Plan
	return [2]source.Target{{Position: storage.Position{Instance: p.First, Slot: 1}, Generation: 2, CandidateID: "first"}, {Position: storage.Position{Instance: p.Second, Slot: 1}, Generation: 2, CandidateID: "second"}}, w.old, w.step("plan-copies")
}
func (w *changedWorld) Build(_ context.Context, t source.Target, m candidate.Manifest, force bool) (candidate.Report, error) {
	if force || !reflect.DeepEqual(m, w.prepared.Manifest) {
		w.t.Fatal("wrong build baseline/mode")
	}
	err := w.step("build-" + t.CandidateID)
	return candidate.Report{StorageComplete: err == nil}, err
}
func (w *changedWorld) Validate(ctx context.Context, t source.Target, m candidate.Manifest) (candidate.Evidence, error) {
	if err := w.step("ready-" + t.CandidateID); err != nil {
		return candidate.Evidence{}, err
	}
	fake := &initialWorld{t: w.t, input: m}
	return fake.Validate(ctx, t, m)
}
func (w *changedWorld) Check(_ context.Context, p control.UpdatePlan, next, old update.Copy) (update.ReadabilityProof, error) {
	if err := w.step("old-readable"); err != nil {
		return update.ReadabilityProof{}, err
	}
	return update.ReadabilityProof{TaskID: p.TaskID, Origin: p.Origin, Destination: control.SlotIdentity{Position: old.Target.Position, Generation: old.Target.Generation, BundleID: old.Manifest.Combination}, Candidate: next.Target, Combination: next.Manifest.Combination, CheckedAt: p.StartedAt}, nil
}
func (w *changedWorld) Shift(_ context.Context, p update.ReadabilityProof) (update.ShiftResult, error) {
	if err := w.step("shift"); err != nil {
		return update.ShiftResult{}, err
	}
	a := p.Origin
	a.Sequence++
	a.EventID = p.TaskID + "-shift"
	a.TaskID = p.TaskID
	a.Position = p.Destination
	w.active = a
	raw, _ := control.ActiveDocument(a)
	expected, _ := control.ActiveDocument(p.Origin)
	e := control.Publication{ID: a.EventID, Sequence: a.Sequence, Kind: "route_shift", TaskID: p.TaskID, Status: "confirmed", Target: raw, Expected: &expected, CreatedAt: p.CheckedAt, ConfirmedAt: &p.CheckedAt}
	err := w.step("drain")
	return update.ShiftResult{Event: e, Drained: err == nil}, err
}
func (w *changedWorld) ConfirmCopies(context.Context, control.UpdatePlan, [2]update.Copy) error {
	return w.step("both-ready")
}
func (w *changedWorld) StageProgress(_ context.Context, p control.ProgressPlan) error {
	w.progress = p
	return w.step("progress-plan")
}
func (w *changedWorld) EnqueuePublication(_ context.Context, i control.PublicationIntent) (control.Publication, bool, error) {
	w.publishCalls++
	if i.Kind != "bundle_publish" || i.Expected == nil || *i.Expected != w.active || i.Target.Position.Instance != w.prepared.Plan.First || i.Target.BundleID != "C101" {
		w.t.Fatal("wrong publication intent", i)
	}
	return control.Publication{ID: i.ID, Status: "pending"}, true, w.step("publish-enqueue")
}
func (w *changedWorld) Apply(context.Context, control.Publication) error {
	return w.step("publish-apply")
}
func (w *changedWorld) ConfirmPublication(_ context.Context, e control.Publication, _ time.Time) (control.Publication, error) {
	if err := w.step("publish-confirm"); err != nil {
		return control.Publication{}, err
	}
	e.Status = "confirmed"
	return e, nil
}
func (w *changedWorld) CompletePublished(context.Context, control.Publication, time.Time) error {
	return w.step("progress-success")
}
func changedFixture(t *testing.T) (update.Changed, *changedWorld) {
	t.Helper()
	p := updatePlan()
	next := copyFixture(t, "C101", 1)
	old := copyFixture(t, "C100", 0)
	w := &changedWorld{t: t, prepared: update.PreparedUpdate{Plan: p, Manifest: next.Manifest}, old: old, active: p.Origin}
	w.prepared.Changes = map[string]candidate.ChangeCounts{}
	for _, v := range next.Manifest.Tables {
		w.prepared.Changes[v.Table] = candidate.ChangeCounts{Before: v.Rows, After: v.Rows}
	}
	c := update.Changed{Source: w, Store: w, Builder: w, Validator: w, Readability: w, Shifter: w, Outbox: w, Delivery: w, Progress: w, Clock: testkit.FakeClock{Time: p.StartedAt.Add(13 * time.Minute)}}
	return c, w
}
func TestT13_07ChangedRoundBuildsAndPublishesInStrictOrderBothDirections(t *testing.T) {
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		t.Run(string(instance), func(t *testing.T) {
			c, w := changedFixture(t)
			if instance == storage.Standby {
				w.prepared.Plan.First = storage.Main
				w.prepared.Plan.Second = storage.Standby
				w.prepared.Plan.Origin.Position.Position.Instance = storage.Standby
				w.old.Target.Position.Instance = storage.Main
				w.old.Evidence.Target = w.old.Target
				w.active = w.prepared.Plan.Origin
			}
			out, err := c.Run(t.Context(), w.active)
			want := []string{"acquire-compare", "plan-copies", "build-first", "ready-first", "old-readable", "shift", "drain", "build-second", "ready-second", "both-ready", "progress-plan", "publish-enqueue", "publish-apply", "publish-confirm", "progress-success"}
			if err != nil || !reflect.DeepEqual(w.trace, want) || out.Copies[1].Target.Position.Instance != instance || out.Event.Status != "confirmed" {
				t.Fatal(err, w.trace, out)
			}
		})
	}
}
func TestT13_11SecondCopyFailureKeepsShiftedOldCombinationWithoutPublication(t *testing.T) {
	for _, fail := range []string{"build-second", "ready-second", "both-ready"} {
		t.Run(fail, func(t *testing.T) {
			c, w := changedFixture(t)
			w.fail = fail
			out, err := c.Run(t.Context(), w.active)
			if err == nil || w.publishCalls != 0 || w.active.Position.BundleID != "C100" || w.active.Position.Position.Instance != storage.Standby || out.Event.ID != "" || w.progress.TaskID != "" {
				t.Fatal(out, err, w.active)
			}
		})
	}
}
func TestT13_04And09And12EarlierFailurePreventsSecondBuildAndPublish(t *testing.T) {
	for _, fail := range []string{"acquire-compare", "plan-copies", "build-first", "ready-first", "old-readable", "shift", "drain"} {
		t.Run(fail, func(t *testing.T) {
			c, w := changedFixture(t)
			w.fail = fail
			_, err := c.Run(t.Context(), w.active)
			if err == nil || w.publishCalls != 0 || w.progress.TaskID != "" {
				t.Fatal(err, w.trace)
			}
			for _, s := range w.trace {
				if s == "build-second" {
					t.Fatal(w.trace)
				}
			}
			if fail == "acquire-compare" && len(w.trace) != 1 {
				t.Fatal("failed source continued", w.trace)
			}
			if fail == "old-readable" && w.active != w.prepared.Plan.Origin {
				t.Fatal("bad old copy shifted")
			}
		})
	}
}
