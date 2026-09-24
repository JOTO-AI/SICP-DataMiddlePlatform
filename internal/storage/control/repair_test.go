package control_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type repairDB struct {
	*readerDB
	active         control.Document
	pending, ready bool
	candidate      *control.Candidate
}
type repairTX struct {
	*readerTX
	d         *repairDB
	candidate *control.Candidate
}

func (d *repairDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	base, err := d.readerDB.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return &repairTX{readerTX: base.(*readerTX), d: d, candidate: d.candidate}, nil
}
func (x *repairTX) QueryRow(ctx context.Context, sql string, args ...any) storage.Row {
	switch {
	case strings.Contains(sql, "publication_outbox WHERE status='pending'"):
		return result{raw: []any{x.d.pending}}
	case strings.HasPrefix(sql, "SELECT (SELECT target::text"):
		return result{raw: []any{string(x.d.active)}}
	case strings.Contains(sql, "SELECT EXISTS (SELECT 1 FROM control.candidates"):
		if len(args) == 1 {
			return result{raw: []any{x.candidate != nil}}
		}
		return result{raw: []any{x.d.ready}}
	case strings.Contains(sql, "FROM control.candidates WHERE"):
		return result{raw: entityColumns(*x.candidate)}
	default:
		return x.readerTX.QueryRow(ctx, sql, args...)
	}
}
func (x *repairTX) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if strings.HasPrefix(sql, "INSERT INTO control.candidates") {
		x.candidate = &control.Candidate{ID: args[0].(string), TaskID: args[1].(string), Instance: args[2].(string), Slot: args[3].(int16), Generation: args[4].(int64), BundleID: args[5].(string), OwnerGeneration: args[6].(int64), State: "building"}
		return 1, nil
	}
	if strings.HasPrefix(sql, "UPDATE control.slots SET admission_open=TRUE") {
		x.d.slot.AdmissionOpen = true
		return 1, nil
	}
	if strings.Contains(sql, "state='building'") {
		bundle := x.d.slot.BundleID
		n, err := x.readerTX.Exec(ctx, sql, args...)
		x.d.slot.BundleID = bundle
		return n, err
	}
	return x.readerTX.Exec(ctx, sql, args...)
}
func (x *repairTX) Commit(ctx context.Context) error {
	if err := x.readerTX.Commit(ctx); err != nil {
		return err
	}
	x.d.candidate = x.candidate
	return nil
}
func repairRepository(t *testing.T) (control.Repository, *repairDB, control.ActiveMetadata, control.SlotIdentity) {
	r, d, h := readerRepository(t)
	active := control.ActiveMetadata{Sequence: 1, EventID: "original", TaskID: "original-task", OwnerGeneration: 1, Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 3, BundleID: h.BundleID}, CodeVersion: "v1"}
	doc, err := control.ActiveDocument(active)
	if err != nil {
		t.Fatal(err)
	}
	db := &repairDB{readerDB: d, active: doc}
	r.Database = db
	return writer(r), db, active, h
}

func TestT14_12RepairIsolationDrainGenerationAndReadinessGates(t *testing.T) {
	r, d, active, h := repairRepository(t)
	if err := r.IsolateRepair(t.Context(), active, h); err != nil || d.slot.AdmissionOpen {
		t.Fatal(err)
	}
	d.readers = 1
	if _, err := r.PlanRepair(t.Context(), active, h, "repair"); err == nil || d.slot.Generation != h.Generation {
		t.Fatal("repair with reader", err)
	}
	d.readers = 0
	target, err := r.PlanRepair(t.Context(), active, h, "repair")
	if err != nil || target.Generation != h.Generation+1 || d.slot.BundleID == nil || *d.slot.BundleID != h.BundleID || d.slot.AdmissionOpen {
		t.Fatal(target, err)
	}
	again, err := r.PlanRepair(t.Context(), active, h, "repair")
	if err != nil || again != target {
		t.Fatal(again, err)
	}
	if err = r.OpenRepaired(t.Context(), active, target, "v1"); err == nil || d.slot.AdmissionOpen {
		t.Fatal("unready repair admitted")
	}
	d.slot.State = "ready"
	d.ready = true
	if err = r.OpenRepaired(t.Context(), active, target, "v1"); err != nil || !d.slot.AdmissionOpen {
		t.Fatal(err)
	}
	if _, err = r.RegisterReader(t.Context(), h, "late", "query", time.Now()); err == nil {
		t.Fatal("old generation admitted after repair")
	}
}

func TestT14_12RepairRefusesPendingServingChangedRouteAndCommitFailure(t *testing.T) {
	for _, failure := range []string{"pending", "serving", "changed", "commit"} {
		t.Run(failure, func(t *testing.T) {
			r, d, active, h := repairRepository(t)
			switch failure {
			case "pending":
				d.pending = true
			case "serving":
				active.Position.Position.Instance = h.Position.Instance
			case "changed":
				active.Sequence++
			case "commit":
				d.failCommit = true
			}
			if err := r.IsolateRepair(t.Context(), active, h); err == nil || !d.slot.AdmissionOpen || d.slot.Generation != h.Generation {
				t.Fatal("unsafe isolation", err)
			}
		})
	}
}
