package update_test

import (
	"context"
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type unchangedWorld struct {
	copies    [2]update.Copy
	checks    int
	failAt    int
	staged    bool
	confirmed bool
	plan      control.ProgressPlan
}

func (w *unchangedWorld) LoadPublished(context.Context, control.UpdatePlan) ([2]update.Copy, error) {
	return w.copies, nil
}
func (w *unchangedWorld) VerifyPublished(_ context.Context, p control.UpdatePlan, _ update.Copy) (time.Time, error) {
	w.checks++
	if w.failAt == w.checks {
		return time.Time{}, errors.New("synthetic corrupt copy")
	}
	return p.StartedAt.Add(time.Minute), nil
}
func (w *unchangedWorld) StageProgress(_ context.Context, p control.ProgressPlan) error {
	w.staged = true
	w.plan = p
	return nil
}
func (w *unchangedWorld) CompleteUnchanged(context.Context, control.ActiveMetadata, [2]control.SlotIdentity, string, time.Time) error {
	w.confirmed = true
	return nil
}
func noChangeFixture(t *testing.T) (update.Unchanged, *unchangedWorld, update.PreparedUpdate) {
	a := copyFixture(t, "C100", 0)
	a.Target.Position.Instance = storage.Main
	a.Evidence.Target = a.Target
	b := copyFixture(t, "C100", 0)
	w := &unchangedWorld{copies: [2]update.Copy{a, b}}
	u := update.Unchanged{Copies: w, Verifier: w, Progress: w, Clock: testkit.FakeClock{Time: updatePlan().StartedAt.Add(2 * time.Minute)}, CodeVersion: "v1"}
	in := update.PreparedUpdate{Plan: updatePlan(), Manifest: copyFixture(t, "C101", 1).Manifest}
	in.Changes = map[string]candidate.ChangeCounts{}
	for _, v := range in.Manifest.Tables {
		in.Changes[v.Table] = candidate.ChangeCounts{Before: v.Rows, After: v.Rows}
	}
	return u, w, in
}
func TestT13_13EmptyOrSameContentUsesBothCopiesWithoutNewCombination(t *testing.T) {
	for _, rows := range []int64{0, 3} {
		t.Run(time.Duration(rows).String(), func(t *testing.T) {
			u, w, in := noChangeFixture(t)
			in.Manifest.Tables[0].Rows = rows
			in.Changes[in.Manifest.Tables[0].Table] = candidate.ChangeCounts{Before: rows, After: rows}
			for n := range w.copies {
				w.copies[n].Manifest.Tables[0].Rows = rows
			}
			ok, err := u.Try(t.Context(), in)
			if err != nil || !ok || w.checks != 2 || !w.staged || !w.confirmed || w.plan.BundleID != "C100" || len(w.plan.Ends) != 14 || !w.plan.StartedAt.Equal(in.Plan.StartedAt) {
				t.Fatal(ok, err, w)
			}
		})
	}
}
func TestT13_14DamagedOrStaleSecondCopyCannotConfirmNoChange(t *testing.T) {
	for _, kind := range []string{"corrupt", "code", "content", "identity"} {
		t.Run(kind, func(t *testing.T) {
			u, w, in := noChangeFixture(t)
			switch kind {
			case "corrupt":
				w.failAt = 2
			case "code":
				w.copies[1].Evidence.CodeVersion = "stale"
			case "content":
				w.copies[1].Manifest.Tables[0].Rows++
			case "identity":
				w.copies[1].Target.Generation++
			}
			ok, err := u.Try(t.Context(), in)
			if err == nil || ok || w.staged || w.confirmed {
				t.Fatal(ok, err, w)
			}
		})
	}
}
func TestT13_13ChangedTargetDoesNotTakeUnchangedPath(t *testing.T) {
	u, w, in := noChangeFixture(t)
	in.Manifest.Tables[0].Rows++
	ok, err := u.Try(t.Context(), in)
	if err != nil || ok || w.checks != 0 || w.confirmed {
		t.Fatal(ok, err)
	}
}

func TestT13_13RunnerReusesCombinationWithoutCallingChangedPipeline(t *testing.T) {
	u, w, in := noChangeFixture(t)
	_, source := changedFixture(t)
	source.prepared = in
	out, err := (update.Runner{Source: source, Unchanged: u}).Run(t.Context(), in.Plan.Origin)
	if err != nil || !out.Unchanged || !w.confirmed || len(source.trace) != 1 || source.publishCalls != 0 {
		t.Fatal(out, err)
	}
}
