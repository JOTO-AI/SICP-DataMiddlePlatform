package control_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type drainDB struct {
	base   *publicationDB
	busy   bool
	closed bool
	locks  []int32
}
type drainTx struct {
	*publicationTx
	db     *drainDB
	closed bool
}

func (d *drainDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.base.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return &drainTx{publicationTx: tx.(*publicationTx), db: d, closed: d.closed}, nil
}
func (t *drainTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if strings.Contains(sql, "pg_advisory_xact_lock") {
		t.db.locks = append(t.db.locks, args[1].(int32))
		return 1, nil
	}
	if sql == "UPDATE control.slots SET admission_open=FALSE WHERE instance=$1" {
		if args[0] != "M" {
			return 0, control.ErrEntity
		}
		t.closed = true
		return 2, nil
	}
	return t.publicationTx.Exec(ctx, sql, args...)
}
func (t *drainTx) QueryRow(ctx context.Context, sql string, args ...any) storage.Row {
	if strings.Contains(sql, "FROM control.readers") {
		if args[0] != "M" || strings.Contains(sql, "slot=") || !strings.Contains(sql, "released_at IS NULL OR sql_finished_at IS NULL") {
			return result{fail: true}
		}
		return result{raw: []any{t.db.busy}}
	}
	return t.publicationTx.QueryRow(ctx, sql, args...)
}
func (t *drainTx) Commit(ctx context.Context) error {
	if err := t.publicationTx.Commit(ctx); err != nil {
		return err
	}
	t.db.closed = t.closed
	return nil
}
func drainFixture(t *testing.T) (control.Repository, *drainDB, control.Publication) {
	t.Helper()
	r, base, _ := publicationFixture(t)
	at := time.Date(2100, 1, 1, 10, 0, 0, 0, time.UTC)
	origin := control.ActiveMetadata{Sequence: 1, EventID: "old", TaskID: "old-task", OwnerGeneration: 1, Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 1, BundleID: "C100"}}
	expected, _ := control.ActiveDocument(origin)
	next := origin
	next.Sequence = 2
	next.EventID = "task-A-shift"
	next.TaskID = "task-A"
	next.OwnerGeneration = 9
	next.Position.Position.Instance = storage.Standby
	target, _ := control.ActiveDocument(next)
	e := control.Publication{ID: next.EventID, Sequence: 2, TaskID: "task-A", Kind: "route_shift", Expected: &expected, Target: target, Status: "confirmed", CreatedAt: at, AppliedAt: &at, ConfirmedAt: &at}
	base.events[e.ID] = e
	base.confirmed = &target
	d := &drainDB{base: base}
	r.Database = d
	return r, d, e
}
func TestT13_10DrainClosesBothOriginalSlotsAndWaitsForAnyActualSQL(t *testing.T) {
	r, d, e := drainFixture(t)
	d.busy = true
	ok, err := r.DrainShift(t.Context(), e)
	if err != nil || ok || !d.closed || len(d.locks) != 2 || d.locks[0] != 0 || d.locks[1] != 1 {
		t.Fatal(ok, err, d)
	}
	d.busy = false
	ok, err = r.DrainShift(t.Context(), e)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
}
func TestT13_09DrainRejectsUnconfirmedStaleOrUncommittedShift(t *testing.T) {
	for _, kind := range []string{"pending", "unapplied", "latest-changed", "new-pending", "commit", "owner"} {
		t.Run(kind, func(t *testing.T) {
			r, d, e := drainFixture(t)
			switch kind {
			case "pending":
				e.Status = "pending"
				d.base.events[e.ID] = e
			case "unapplied":
				e.AppliedAt = nil
				d.base.events[e.ID] = e
			case "latest-changed":
				d.base.confirmed = e.Expected
			case "new-pending":
				d.base.events["new"] = control.Publication{Status: "pending"}
			case "commit":
				d.base.failCommit = true
			case "owner":
				r = r.WithOwnership(control.Ownership{TaskID: "task-A", OwnerID: "old", Generation: 8})
			}
			ok, err := r.DrainShift(t.Context(), e)
			if err == nil || ok || d.closed {
				t.Fatal(ok, err, d.closed)
			}
		})
	}
}
