package control_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestT15_10RuntimePhysicalFenceLocksOnlyItsTargetUntilSQLReturns(t *testing.T) {
	r, d := repository(t)
	r = writer(r)
	tx, e := r.BeginFenced(t.Context(), storage.Position{Instance: storage.Standby, Slot: 1})
	if e != nil {
		t.Fatal(e)
	}
	if len(d.calls) != 2 || !strings.Contains(d.calls[0].sql, "control.execution") || len(d.calls[1].args) != 2 || d.calls[1].args[1] != int32(3) {
		t.Fatal(d.calls)
	}
	if d.rollbacks != 0 {
		t.Fatal("physical fence released early")
	}
	if e = tx.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
}
func TestT15_10RuntimeFenceRejectsOldOwnerAndRollsBackFailedFacts(t *testing.T) {
	r, d := repository(t)
	called := false
	e := r.WithOwnership(control.Ownership{TaskID: "task-A", OwnerID: "old", Generation: 8}).Fenced(t.Context(), func(storage.Transaction) error { called = true; return nil })
	if !errors.Is(e, control.ErrOwnership) || called || d.rollbacks != 1 {
		t.Fatal(e, called, d.rollbacks)
	}
	r = writer(r)
	bad := errors.New("injected fact failure")
	e = r.Fenced(t.Context(), func(tx storage.Transaction) error {
		called = true
		_, e := tx.Exec(context.Background(), "synthetic operation")
		if e != nil {
			return e
		}
		return bad
	})
	if !errors.Is(e, bad) || !called || d.rollbacks != 2 {
		t.Fatal(e, d.rollbacks)
	}
	d.failCommit = true
	if e = r.Fenced(t.Context(), func(storage.Transaction) error { return nil }); e == nil {
		t.Fatal("commit failure hidden")
	}
}
