package update_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type repairWorld struct {
	t                *testing.T
	trace            []string
	fail             string
	isolated, opened bool
	input            candidate.Manifest
}

func (w *repairWorld) step(s string) error {
	w.trace = append(w.trace, s)
	if s == w.fail {
		return errors.New("repair failed")
	}
	return nil
}
func (w *repairWorld) IsolateRepair(context.Context, control.ActiveMetadata, control.SlotIdentity) error {
	err := w.step("isolate")
	if err == nil {
		w.isolated = true
	}
	return err
}
func (w *repairWorld) PlanRepair(_ context.Context, _ control.ActiveMetadata, h control.SlotIdentity, id string) (source.Target, error) {
	return source.Target{Position: h.Position, Generation: h.Generation + 1, CandidateID: id}, w.step("plan")
}
func (w *repairWorld) OpenRepaired(context.Context, control.ActiveMetadata, source.Target) error {
	err := w.step("open")
	if err == nil {
		w.opened = true
	}
	return err
}
func (w *repairWorld) Validate(_ context.Context, target source.Target, m candidate.Manifest) (candidate.Evidence, error) {
	if err := w.step("validate"); err != nil {
		return candidate.Evidence{}, err
	}
	return (&initialWorld{t: w.t, input: m}).Validate(context.Background(), target, m)
}
func (w *repairWorld) Build(_ context.Context, _ source.Target, m candidate.Manifest, force bool) (candidate.Report, error) {
	if !w.isolated || !force || !reflect.DeepEqual(m, w.input) {
		w.t.Fatal("unsafe or wrong-combination repair")
	}
	err := w.step("build")
	return candidate.Report{StorageComplete: err == nil}, err
}

func TestT14_12StandbyRepairPreservesCombinationAndOpensOnlyAfterValidation(t *testing.T) {
	for _, failure := range []string{"", "isolate", "source", "plan", "build", "validate", "open"} {
		t.Run("failure-"+failure, func(t *testing.T) {
			damaged := copyFixture(t, "C100", 1)
			active := updatePlan().Origin
			w := &repairWorld{t: t, fail: failure, input: damaged.Manifest}
			full := &recoveredTarget{}
			if failure == "source" {
				full.fail = errors.New("untrusted target")
			}
			r := update.Repairer{Store: w, Source: full, Builder: w, Validator: w}
			out, err := r.Repair(t.Context(), active, damaged, "repair-copy")
			if failure == "" {
				if err != nil || !w.opened || out.Manifest.Combination != "C100" || out.Target.Generation != damaged.Target.Generation+1 || !reflect.DeepEqual(w.trace, []string{"isolate", "plan", "build", "validate", "open"}) {
					t.Fatal(out, err, w.trace)
				}
			} else if err == nil || w.opened || out.Target.CandidateID != "" {
				t.Fatal("failed repair opened damaged copy", out, err)
			}
		})
	}
}
func TestT14_12RepairRejectsServingInstanceOrDifferentCombination(t *testing.T) {
	for _, wrong := range []string{"instance", "combination"} {
		t.Run(wrong, func(t *testing.T) {
			damaged := copyFixture(t, "C100", 1)
			active := updatePlan().Origin
			w := &repairWorld{t: t}
			if wrong == "instance" {
				damaged.Target.Position.Instance = active.Position.Position.Instance
			} else {
				damaged.Manifest.Combination = "C101"
			}
			r := update.Repairer{Store: w, Source: &recoveredTarget{}, Builder: w, Validator: w}
			if _, err := r.Repair(t.Context(), active, damaged, "repair"); err == nil || len(w.trace) != 0 {
				t.Fatal("unsafe repair admitted", err)
			}
		})
	}
}
