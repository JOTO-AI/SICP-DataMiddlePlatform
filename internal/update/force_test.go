package update_test

import (
	"context"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type forcedLedger struct {
	world   *changedWorld
	invalid map[string]bool
	writes  map[string]int
}

func (l *forcedLedger) Inspect(_ context.Context, t source.Target) (candidate.Baseline, error) {
	b := candidate.Baseline{State: source.BuildState{ActiveInstance: l.world.active.Position.Position.Instance, Generation: t.Generation, CandidateID: t.CandidateID, OwnerValid: true}, Tables: map[string]candidate.TableEvidence{}}
	// Even perfectly matching tables must be rewritten in forced mode.
	for _, v := range l.world.prepared.Manifest.Tables {
		b.Tables[v.Table] = candidate.TableEvidence{Version: v, Complete: true}
	}
	return b, nil
}
func (l *forcedLedger) Invalidate(_ context.Context, t source.Target, table string) error {
	l.invalid[t.CandidateID+table] = true
	return nil
}
func (l *forcedLedger) Record(_ context.Context, t source.Target, e candidate.TableEvidence) error {
	delete(l.invalid, t.CandidateID+e.Version.Table)
	return nil
}
func (l *forcedLedger) Complete(context.Context, source.Target, candidate.Manifest) error { return nil }
func (l *forcedLedger) Fail(context.Context, source.Target) error                         { return nil }
func (l *forcedLedger) Rebuild(ctx context.Context, t source.Target, table string, it source.Iterator, _ int) (source.Report, error) {
	if !l.invalid[t.CandidateID+table] || t.Position.Instance == l.world.active.Position.Position.Instance {
		return source.Report{}, source.ErrTarget
	}
	l.writes[string(t.Position.Instance)+":"+table]++
	n := 0
	for {
		_, more, err := it.Next(ctx)
		if err != nil {
			return source.Report{}, err
		}
		if !more {
			break
		}
		n++
	}
	return source.Report{WrittenRows: n, Complete: true}, nil
}
func TestT13_15SameContentForcedRoundActuallyRebuildsBothFourteenTableCopies(t *testing.T) {
	i, task, _, _ := initialization(t, "")
	initial, err := i.Run(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := acquisition(t, "")
	p := updatePlan()
	p.Mode = "force_full"
	p.Origin.Position.BundleID = initial.Manifest.Combination
	l, err := a.Acquire(t.Context(), p, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	o := i.Options
	o.Combination = "C101"
	o.Mode = ingest.ForceFull
	o.Base = &initial.Manifest
	o.BaseSource = i.Preparer.(candidate.TargetFiles)
	m, err := i.Preparer.Prepare(t.Context(), o, l)
	if err != nil {
		t.Fatal(err)
	}
	if !update.SameContent(initial.Manifest, m) {
		t.Fatal("fixture is not same-content")
	}
	c, w := changedFixture(t)
	w.prepared = update.PreparedUpdate{Plan: p, Manifest: m, Lease: l}
	w.active = p.Origin
	w.old.Manifest = initial.Manifest
	fake := &initialWorld{t: t, input: initial.Manifest}
	w.old.Evidence, err = fake.Validate(t.Context(), w.old.Target, w.old.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	ledger := &forcedLedger{world: w, invalid: map[string]bool{}, writes: map[string]int{}}
	c.Builder = candidate.Builder{Ledger: ledger, Source: i.Preparer.(candidate.TargetFiles), Writer: ledger, BatchSize: 2}
	u, _, _ := noChangeFixture(t)
	out, err := (update.Runner{Source: w, Unchanged: u, Changed: c}).Run(t.Context(), p.Origin)
	if err != nil || out.Unchanged || len(ledger.writes) != 28 || w.publishCalls != 1 || out.Change.Event.Status != "confirmed" {
		t.Fatal(out, err, ledger.writes)
	}
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		for _, table := range source.Catalog() {
			if ledger.writes[string(instance)+":"+table.Name] != 1 {
				t.Fatal("missing rebuild", instance, table.Name)
			}
		}
	}
	for _, report := range out.Change.Reports {
		if len(report.Rebuilt) != 14 || len(report.Reused) != 0 {
			t.Fatal(report)
		}
	}
}
