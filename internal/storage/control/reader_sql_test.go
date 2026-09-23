package control_test

import (
	"errors"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestT11_12HTTPReleaseAndOldHeartbeatDoNotDrainRunningSQL(t *testing.T) {
	r, d, h := readerRepository(t)
	at := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	if _, err := r.RegisterReader(t.Context(), h, "r", "process", at); err != nil {
		t.Fatal(err)
	}
	if err := writer(r).CloseAdmission(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	// A registered reader can finish its batches after new admission closes.
	if err := r.BeginSQL(t.Context(), h, "r", "sql1", at); err != nil {
		t.Fatal(err)
	}
	if err := r.ReleaseReader(t.Context(), h, "r", at.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if drained, err := r.Drained(t.Context(), h); err != nil || drained {
		t.Fatal("live SQL drained", drained, err)
	}
	if _, err := writer(r).ReserveOverwrite(t.Context(), h); !errors.Is(err, control.ErrBusy) {
		t.Fatal(err)
	}
	if d.records["r"].SQLFinishedAt != nil || d.records["r"].ReleasedAt == nil {
		t.Fatal("HTTP release forged SQL end")
	}
	if err := r.BeginSQL(t.Context(), h, "r", "sql2", at.Add(24*time.Hour)); !errors.Is(err, control.ErrBusy) {
		t.Fatal("released reader started SQL", err)
	}
	if err := r.FinishSQL(t.Context(), h, "r", "sql1", at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if drained, err := r.Drained(t.Context(), h); err != nil || !drained {
		t.Fatal(drained, err)
	}
}

func TestT11_13DuplicateFinishCannotEndAnotherSQLAndReleaseIsIdempotent(t *testing.T) {
	r, d, h := readerRepository(t)
	at := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	if _, err := r.RegisterReader(t.Context(), h, "r", "process", at); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"sql1", "sql2"} {
		if err := r.BeginSQL(t.Context(), h, "r", id, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer(r).CloseAdmission(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := r.ReleaseReader(t.Context(), h, "r", at); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := r.FinishSQL(t.Context(), h, "r", "sql1", at); err != nil {
			t.Fatal(err)
		}
	}
	if drained, err := r.Drained(t.Context(), h); err != nil || drained || d.records["r"].SQLFinishedAt != nil {
		t.Fatal("duplicate ended other SQL", drained, err)
	}
	if err := r.FinishSQL(t.Context(), h, "r", "sql2", at); err != nil {
		t.Fatal(err)
	}
	if drained, err := r.Drained(t.Context(), h); err != nil || !drained {
		t.Fatal(drained, err)
	}
	if _, err := writer(r).ReserveOverwrite(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	// Late idempotent releases bind to the old reader, never a new generation.
	if err := r.ReleaseReader(t.Context(), h, "r", at); err != nil {
		t.Fatal(err)
	}
	if d.slot.Generation != 8 || d.slot.State != "building" {
		t.Fatal(d.slot)
	}
}
