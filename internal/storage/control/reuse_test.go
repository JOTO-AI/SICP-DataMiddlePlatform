package control_test

import (
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestT14_16CrossRoundReuseAdvancesGenerationAndInvalidatesOldReaders(t *testing.T) {
	r, d, active, h := repairRepository(t)
	h.BundleID = "C100"
	d.slot.BundleID = &h.BundleID
	active.Position.BundleID = "C101"
	d.active, _ = control.ActiveDocument(active)
	old := h
	target, err := r.ReuseSlot(t.Context(), active, old, "C102", "candidate-C102")
	if err != nil || target.Generation != old.Generation+1 || *d.slot.BundleID != "C102" || d.slot.AdmissionOpen || d.candidate.BundleID != "C102" {
		t.Fatal(target, err)
	}
	again, err := r.ReuseSlot(t.Context(), active, old, "C102", "candidate-C102")
	if err != nil || again != target {
		t.Fatal("duplicate reservation changed generation", again, err)
	}
	if _, err = r.RegisterReader(t.Context(), old, "late", "query", time.Now()); err == nil {
		t.Fatal("C100 reader read reused C102 slot")
	}
}

func TestT14_16ReuseRejectsReferencesServingInstancePendingAndCommitFailure(t *testing.T) {
	for _, failure := range []string{"reference", "snapshot", "sql", "serving", "pending", "commit"} {
		t.Run(failure, func(t *testing.T) {
			r, d, active, h := repairRepository(t)
			h.BundleID = "C100"
			d.slot.BundleID = &h.BundleID
			active.Position.BundleID = "C101"
			d.active, _ = control.ActiveDocument(active)
			switch failure {
			case "reference":
				d.referenced = true
			case "snapshot":
				d.retained = true
			case "sql":
				now := time.Now()
				d.records = map[string]control.Reader{"sql": {ReleasedAt: &now}}
			case "serving":
				active.Position.Position.Instance = h.Position.Instance
			case "pending":
				d.pending = true
			case "commit":
				d.failCommit = true
			}
			if target, err := r.ReuseSlot(t.Context(), active, h, "C102", "next"); err == nil || target.CandidateID != "" || d.slot.Generation != h.Generation || *d.slot.BundleID != h.BundleID {
				t.Fatal("unsafe reuse", target, err)
			}
		})
	}
}

func TestT14_14LowLevelOverwriteAlsoHonorsRecoveryReferences(t *testing.T) {
	r, d, _, h := repairRepository(t)
	d.referenced = true
	if _, err := r.ReserveOverwrite(t.Context(), h); err == nil || d.slot.Generation != h.Generation {
		t.Fatal("low-level overwrite ignored reference")
	}
}
