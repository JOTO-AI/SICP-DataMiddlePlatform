package control_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestT11_14SnapshotPinProtectsLastReadableCopy(t *testing.T) {
	r, d, h := readerRepository(t)
	d.retained = true
	if _, err := writer(r).ReserveOverwrite(t.Context(), h); !errors.Is(err, control.ErrBusy) {
		t.Fatal("last copy overwritten", err)
	}
	d.otherCopy = true
	if _, err := writer(r).ReserveOverwrite(t.Context(), h); err != nil {
		t.Fatal("same version move blocked", err)
	}
}
func TestT11_16SnapshotPersistenceUsesPublishedReadableBundleAndBoundValues(t *testing.T) {
	r, d := repository(t)
	at := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	s := control.Snapshot{Token: strings.Repeat("a", 64), BundleID: "C1", Operation: "students.grades.query", Conditions: "query:C1:students.grades.query:page=1&student_number=001", CreatedAt: at, ExpiresAt: at.Add(5 * time.Minute)}
	if err := r.PutSnapshot(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 2 || !strings.Contains(d.calls[0].sql, "pg_advisory_xact_lock") || !strings.Contains(d.calls[1].sql, "published_at IS NOT NULL") || !strings.Contains(d.calls[1].sql, "admission_open=TRUE") || d.calls[1].args[0] != s.Token || d.calls[1].args[3] != s.Conditions {
		t.Fatal(d.calls)
	}
	d.raw = []any{s.BundleID, s.Operation, s.Conditions, s.CreatedAt, s.ExpiresAt}
	got, err := r.GetSnapshot(t.Context(), s.Token)
	if err != nil || got != s {
		t.Fatal(got, err)
	}
	d.writeCount = 0
	if err = r.PutSnapshot(t.Context(), s); err == nil {
		t.Fatal("unpublished bundle accepted")
	}
}
