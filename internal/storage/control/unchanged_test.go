package control_test

import (
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func unchangedFixture(t *testing.T) (control.Repository, *progressDB, control.ActiveMetadata, [2]control.SlotIdentity) {
	t.Helper()
	r, d, p, _ := progressFixture(t)
	p.Mode = "incremental"
	d.task.Mode = p.Mode
	d.base.unpublished = false
	d.base.slot.AdmissionOpen = true
	a := control.ActiveMetadata{Sequence: 1, EventID: "old", TaskID: "old-task", OwnerGeneration: 1, CodeVersion: "query-v1", Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 8, BundleID: p.BundleID}}
	doc, _ := control.ActiveDocument(a)
	d.base.confirmed = &doc
	copies := [2]control.SlotIdentity{a.Position, a.Position}
	copies[0].Position.Instance = storage.Main
	if err := r.StageProgress(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	return r, d, a, copies
}
func TestT13_13UnchangedConfirmationAtomicallyAdvancesChecksAndProgressWithoutPublishing(t *testing.T) {
	r, d, a, copies := unchangedFixture(t)
	at := d.task.StartedAt.Add(time.Hour)
	before := *d.base.confirmed
	if err := r.CompleteUnchanged(t.Context(), a, copies, "query-v1", at); err != nil {
		t.Fatal(err)
	}
	if d.task.Status != "success_unchanged" || len(d.progress) != 14 || len(d.checks) != 14 || d.published != nil || d.task.PublishedAt != nil || *d.base.confirmed != before || len(d.base.events) != 0 {
		t.Fatal("no-change altered publication or partial progress")
	}
	for _, p := range d.progress {
		if !p.ConfirmedUntil.Equal(*d.task.StartedAt) {
			t.Fatal(p)
		}
	}
	if err := r.CompleteUnchanged(t.Context(), a, copies, "query-v1", at.Add(time.Hour)); err != nil || !d.task.FinishedAt.Equal(at) {
		t.Fatal("replay changed end time", err)
	}
}
func TestT13_14UnchangedFaultNeverCommitsPartialSuccess(t *testing.T) {
	for _, kind := range []string{"copy-not-ready", "copy-closed", "wrong-generation", "one-instance", "pending", "wrong-active", "partial-progress", "commit"} {
		t.Run(kind, func(t *testing.T) {
			r, d, a, copies := unchangedFixture(t)
			switch kind {
			case "copy-not-ready":
				d.base.ready = false
			case "copy-closed":
				d.base.slot.AdmissionOpen = false
			case "wrong-generation":
				copies[0].Generation++
			case "one-instance":
				copies[0] = copies[1]
			case "pending":
				d.base.events["pending"] = control.Publication{Status: "pending"}
			case "wrong-active":
				a.Sequence++
			case "partial-progress":
				d.failAt = 7
			case "commit":
				d.base.failCommit = true
			}
			if err := r.CompleteUnchanged(t.Context(), a, copies, "query-v1", d.task.StartedAt.Add(time.Hour)); err == nil || len(d.progress) != 0 || len(d.checks) != 0 || d.task.Status != "running" {
				t.Fatal(err, d.task)
			}
		})
	}
}
