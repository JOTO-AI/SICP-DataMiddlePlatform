package control_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestT03_17CooldownUsesFixedControlAndMonotonicSQL(t *testing.T) {
	r, d := repository(t)
	until := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	if err := r.ExtendCooldown(context.Background(), until); err != nil {
		t.Fatal(err)
	}
	if d.calls[0].sql != "UPDATE control.schedule SET cooldown_until=GREATEST(cooldown_until,$1) WHERE singleton=true" || len(d.calls[0].args) != 1 || d.calls[0].args[0] != until {
		t.Fatal("cooldown could move backwards or changed scheduling fields")
	}
	d.raw = []any{until}
	got, err := r.Cooldown(context.Background())
	if err != nil || !got.Equal(until) {
		t.Fatal("cooldown read failed")
	}
	d.raw = []any{nil}
	got, err = r.Cooldown(context.Background())
	if err != nil || !got.IsZero() {
		t.Fatal("null cooldown fabricated")
	}
	for _, connection := range d.connections {
		if connection != "postgres://control@m:5432/control?sslmode=disable" {
			t.Fatal("cooldown followed active business route")
		}
	}
	d.failCommit = true
	d.raw = []any{until}
	got, err = r.Cooldown(context.Background())
	if err == nil || !got.IsZero() {
		t.Fatal("failed read commit exposed value")
	}
	if err = r.ExtendCooldown(context.Background(), until); err == nil {
		t.Fatal("failed write commit accepted")
	}
	d.failCommit = false
	d.writeCount = 0
	if err = r.ExtendCooldown(context.Background(), until); !errors.Is(err, control.ErrMissing) {
		t.Fatal("missing singleton accepted")
	}
}
