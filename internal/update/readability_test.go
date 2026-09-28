package update_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type readableWorld struct {
	trace []string
	fail  string
	h     read.Handle
	saved []update.ReadabilityProof
}

func (w *readableWorld) step(s string) error {
	w.trace = append(w.trace, s)
	if w.fail == s {
		return errors.New("synthetic old-copy failure")
	}
	return nil
}
func (w *readableWorld) AcquireFixed(_ context.Context, h read.Handle) (read.RequestLease, error) {
	w.h = h
	return w, w.step("admit")
}
func (w *readableWorld) Handle() read.Handle         { return w.h }
func (w *readableWorld) Close(context.Context) error { return w.step("close") }
func (w *readableWorld) SourceStorage(_ context.Context, h read.Handle, m candidate.Manifest) error {
	if h != w.h || m.Combination != "C100" {
		return update.ErrOldCopy
	}
	return w.step("storage")
}
func (w *readableWorld) SourceIndexes(_ context.Context, h read.Handle, _ candidate.Manifest) error {
	if h != w.h {
		return update.ErrOldCopy
	}
	return w.step("permissions")
}
func (w *readableWorld) Check(_ context.Context, h read.Handle, op contract.Operation, _ time.Time) (int, string, error) {
	if h != w.h {
		return 0, "", update.ErrOldCopy
	}
	return 1, strings.Repeat("a", 64), w.step(string(op))
}
func (w *readableWorld) RecordReadability(_ context.Context, p update.ReadabilityProof, _, _ update.Copy) error {
	if err := w.step("record"); err != nil {
		return err
	}
	w.saved = append(w.saved, p)
	return nil
}
func copyFixture(t *testing.T, bundle string, slot int) update.Copy {
	t.Helper()
	m := candidate.Manifest{Combination: bundle, Input: staging.SealRef{RunID: "sealed-" + bundle, SHA256: strings.Repeat("a", 64)}}
	for _, table := range source.Catalog() {
		m.Tables = append(m.Tables, candidate.TableVersion{Table: table.Name, Scope: "mock-school", SHA256: strings.Repeat("b", 64)})
	}
	target := source.Target{Position: storage.Position{Instance: storage.Standby, Slot: slot}, Generation: 1, CandidateID: "candidate-" + bundle}
	w := &initialWorld{t: t, input: m}
	e, err := w.Validate(t.Context(), target, m)
	if err != nil {
		t.Fatal(err)
	}
	return update.Copy{Target: target, Manifest: m, Evidence: e}
}
func readabilityFixture(t *testing.T) (update.Readability, *readableWorld, update.Copy, update.Copy) {
	w := &readableWorld{}
	v := update.Readability{Admission: w, Checks: w, Queries: w, Store: w, Clock: testkit.FakeClock{Time: updatePlan().StartedAt.Add(time.Minute)}}
	return v, w, copyFixture(t, "C101", 1), copyFixture(t, "C100", 0)
}
func TestT13_03OldCopyMustPassIndependentReadAdmissionStoragePermissionsAndQueries(t *testing.T) {
	v, w, next, old := readabilityFixture(t)
	p, err := v.Check(t.Context(), updatePlan(), next, old)
	want := []string{"admit", "storage", "permissions"}
	for _, d := range contract.Definitions() {
		want = append(want, string(d.Operation))
	}
	want = append(want, "close", "record")
	if err != nil || p.Destination.BundleID != "C100" || p.Combination != "C101" || p.Destination.Position.Slot != 0 || !reflect.DeepEqual(w.trace, want) || len(w.saved) != 1 {
		t.Fatal(p, err, w.trace)
	}
}
func TestT13_04MissingOrCorruptOldCopyCannotUseNewCandidateReadiness(t *testing.T) {
	for _, kind := range []string{"missing", "storage", "admit"} {
		t.Run(kind, func(t *testing.T) {
			v, w, next, old := readabilityFixture(t)
			w.fail = kind
			if kind == "missing" {
				old = update.Copy{}
			}
			p, err := v.Check(t.Context(), updatePlan(), next, old)
			if err == nil || p.TaskID != "" || len(w.saved) != 0 {
				t.Fatal(p, err)
			}
		})
	}
}
func TestT13_05OldCopyQueryPrincipalPermissionFailureStopsBeforeRouteShift(t *testing.T) {
	v, w, next, old := readabilityFixture(t)
	w.fail = "permissions"
	p, err := v.Check(t.Context(), updatePlan(), next, old)
	if err == nil || p.TaskID != "" || len(w.saved) != 0 || !reflect.DeepEqual(w.trace, []string{"admit", "storage", "permissions", "close"}) {
		t.Fatal(p, err, w.trace)
	}
}
func TestT13_06WrongOldIdentityOrQueryEvidenceNeverAuthorizesShift(t *testing.T) {
	for _, kind := range []string{"bundle", "generation", "unchecked-evidence", "dependencies", "same-slot", "new-not-ready", "query", "commit", "close"} {
		t.Run(kind, func(t *testing.T) {
			v, w, next, old := readabilityFixture(t)
			switch kind {
			case "bundle":
				old.Manifest.Combination = "C099"
			case "generation":
				old.Target.Generation++
			case "unchecked-evidence":
				old.Evidence.CheckedAt = time.Time{}
			case "dependencies":
				old.Manifest.Tables[0].SHA256 = strings.Repeat("c", 64)
			case "same-slot":
				old.Target.Position = next.Target.Position
				old.Evidence.Target = old.Target
			case "new-not-ready":
				next.Evidence = candidate.Evidence{}
			case "query":
				w.fail = string(contract.Definitions()[0].Operation)
			case "commit":
				w.fail = "record"
			case "close":
				w.fail = "close"
			}
			p, err := v.Check(t.Context(), updatePlan(), next, old)
			if err == nil || p.TaskID != "" || len(w.saved) != 0 {
				t.Fatal(p, err)
			}
		})
	}
}
