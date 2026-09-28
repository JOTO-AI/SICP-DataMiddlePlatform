package runtimecontrol

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func (s Store) state(ctx context.Context, tx storage.Transaction, t source.Target) (source.BuildState, control.Candidate, error) {
	bad := source.BuildState{}
	if _, e := s.task(ctx, tx); e != nil {
		return bad, control.Candidate{}, e
	}
	if e := noPending(ctx, tx); e != nil {
		return bad, control.Candidate{}, e
	}
	c, e := s.candidate(ctx, tx, t)
	if e != nil {
		return bad, c, e
	}
	a, e := current(ctx, tx)
	if e != nil {
		return bad, c, e
	}
	if a != nil && a.Position.Position.Instance == t.Position.Instance {
		return bad, c, control.ErrBusy
	}
	v, e := slot(ctx, tx, t.Position)
	if e != nil {
		return bad, c, e
	}
	if v.Generation != t.Generation || v.BundleID == nil || *v.BundleID != c.BundleID || v.AdmissionOpen {
		return bad, c, control.ErrEntity
	}
	var readers int64
	if e = tx.QueryRow(ctx, "SELECT COUNT(*) FROM control.readers WHERE instance=$1 AND slot=$2 AND (released_at IS NULL OR sql_finished_at IS NULL)", v.Instance, v.Slot).Scan(&readers); e != nil {
		return bad, c, e
	}
	if readers != 0 {
		return bad, c, control.ErrBusy
	}
	b := source.BuildState{Generation: t.Generation, CandidateID: t.CandidateID, OwnerValid: true, Initializing: a == nil}
	if a != nil {
		b.ActiveInstance = a.Position.Position.Instance
	}
	return b, c, nil
}
func (s Store) activate(ctx context.Context, tx storage.Transaction, t source.Target) error {
	c, e := s.candidate(ctx, tx, t)
	if e != nil {
		return e
	}
	v, e := slot(ctx, tx, t.Position)
	if e != nil {
		return e
	}
	if v.Generation == t.Generation {
		_, _, e = s.state(ctx, tx, t)
		return e
	}
	if c.State != "planned" {
		return control.ErrEntity
	}
	if _, e = s.task(ctx, tx); e != nil {
		return e
	}
	if e = noPending(ctx, tx); e != nil {
		return e
	}
	a, e := current(ctx, tx)
	if e != nil {
		return e
	}
	if a != nil && a.Position.Position.Instance == t.Position.Instance {
		return control.ErrBusy
	}
	var p copyPlan
	found, e := s.checkpoint(ctx, tx, "copy-plan", &p)
	if e != nil {
		return e
	}
	if !found {
		return control.ErrEntity
	}
	index := -1
	for n, planned := range p.Targets {
		if planned == t {
			index = n
		}
	}
	if index < 0 || t.Generation <= v.Generation {
		return control.ErrEntity
	}
	previous := p.Previous[index]
	// DrainShift closes both slots of the original query instance after the
	// immutable plan is saved. Closing admission preserves physical identity;
	// reopening it, changing content/state, or advancing generation does not.
	if !v.AdmissionOpen {
		previous.AdmissionOpen = false
	}
	if !reflect.DeepEqual(v, previous) {
		return control.ErrEntity
	}
	var busy bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.readers WHERE instance=$1 AND slot=$2 AND (released_at IS NULL OR sql_finished_at IS NULL)) OR EXISTS (SELECT 1 FROM control.query_snapshots WHERE bundle_id=$3 AND expires_at>CURRENT_TIMESTAMP) OR EXISTS (SELECT 1 FROM control.resource_refs f JOIN control.resources r ON r.id=f.resource_id WHERE r.bundle_id=$3 AND f.released=FALSE)", v.Instance, v.Slot, v.BundleID).Scan(&busy); e != nil {
		return e
	}
	if busy {
		return control.ErrBusy
	}
	// Import only completed physical-table evidence; readiness alone is not a
	// table baseline. Partial writes always invalidate their entry first.
	var raw *string
	if e = tx.QueryRow(ctx, "SELECT (SELECT k.payload::text FROM control.candidates c JOIN control.checkpoints k ON k.task_id=c.task_id AND k.name='ledger-'||c.id WHERE c.instance=$1 AND c.slot=$2 AND c.generation=$3)", v.Instance, v.Slot, v.Generation).Scan(&raw); e != nil {
		return e
	}
	tables := map[string]candidate.TableEvidence{}
	if raw != nil && json.Unmarshal([]byte(*raw), &tables) != nil {
		return control.ErrEntity
	}
	if e = s.save(ctx, tx, "ledger-"+t.CandidateID, tables); e != nil {
		return e
	}
	if e = one(tx.Exec(ctx, "UPDATE control.slots SET generation=$3,bundle_id=$4,admission_open=FALSE,state='building' WHERE instance=$1 AND slot=$2", v.Instance, v.Slot, t.Generation, c.BundleID)); e != nil {
		return e
	}
	return one(tx.Exec(ctx, "UPDATE control.candidates SET state='building',ready_at=NULL,evidence=NULL WHERE id=$1", t.CandidateID))
}
func (s Store) tables(ctx context.Context, tx storage.Transaction, t source.Target) (map[string]candidate.TableEvidence, error) {
	out := map[string]candidate.TableEvidence{}
	_, e := s.checkpoint(ctx, tx, "ledger-"+t.CandidateID, &out)
	return out, e
}
func (s Store) Inspect(ctx context.Context, t source.Target) (out candidate.Baseline, err error) {
	err = s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		if e := s.activate(ctx, tx, t); e != nil {
			return e
		}
		state, _, e := s.state(ctx, tx, t)
		if e != nil {
			return e
		}
		tables, e := s.tables(ctx, tx, t)
		out = candidate.Baseline{State: state, Tables: tables}
		return e
	})
	if err != nil {
		return candidate.Baseline{}, err
	}
	return
}
func (s Store) Invalidate(ctx context.Context, t source.Target, name string) error {
	known := false
	for _, table := range source.Catalog() {
		known = known || table.Name == name
	}
	if !known {
		return control.ErrEntity
	}
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		if _, _, e := s.state(ctx, tx, t); e != nil {
			return e
		}
		tables, e := s.tables(ctx, tx, t)
		if e != nil {
			return e
		}
		delete(tables, name)
		if e = s.save(ctx, tx, "ledger-"+t.CandidateID, tables); e != nil {
			return e
		}
		if e = one(tx.Exec(ctx, "UPDATE control.candidates SET state='building',ready_at=NULL,evidence=NULL WHERE id=$1", t.CandidateID)); e != nil {
			return e
		}
		return one(tx.Exec(ctx, "UPDATE control.slots SET state='building' WHERE instance=$1 AND slot=$2", string(t.Position.Instance), int16(t.Position.Slot)))
	})
}
func (s Store) Record(ctx context.Context, t source.Target, ev candidate.TableEvidence) error {
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		_, c, e := s.state(ctx, tx, t)
		if e != nil {
			return e
		}
		m, e := bundle(ctx, tx, c.BundleID)
		if e != nil {
			return e
		}
		valid := false
		for _, v := range m.Tables {
			valid = valid || v == ev.Version
		}
		if !ev.Complete || !valid {
			return control.ErrEntity
		}
		tables, e := s.tables(ctx, tx, t)
		if e != nil {
			return e
		}
		tables[ev.Version.Table] = ev
		return s.save(ctx, tx, "ledger-"+t.CandidateID, tables)
	})
}
func (s Store) Complete(ctx context.Context, t source.Target, m candidate.Manifest) error {
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		_, c, e := s.state(ctx, tx, t)
		if e != nil {
			return e
		}
		actual, e := bundle(ctx, tx, c.BundleID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(m, actual) {
			return control.ErrEntity
		}
		tables, e := s.tables(ctx, tx, t)
		if e != nil {
			return e
		}
		for _, v := range m.Tables {
			if tables[v.Table] != (candidate.TableEvidence{Version: v, Complete: true}) {
				return control.ErrEntity
			}
		}
		return one(tx.Exec(ctx, "UPDATE control.candidates SET state='storage_complete' WHERE id=$1", t.CandidateID))
	})
}
func (s Store) Fail(ctx context.Context, t source.Target) error {
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		if _, _, e := s.state(ctx, tx, t); e != nil {
			return e
		}
		if e := one(tx.Exec(ctx, "UPDATE control.candidates SET state='failed',ready_at=NULL,evidence=NULL WHERE id=$1", t.CandidateID)); e != nil {
			return e
		}
		return one(tx.Exec(ctx, "UPDATE control.slots SET state='failed',admission_open=FALSE WHERE instance=$1 AND slot=$2", string(t.Position.Instance), int16(t.Position.Slot)))
	})
}

type buildLease struct {
	state source.BuildState
	tx    storage.Transaction
}

func (l *buildLease) State() source.BuildState { return l.state }
func (l *buildLease) Release(ctx context.Context) error {
	defer rollback(ctx, l.tx)
	return l.tx.Commit(ctx)
}
func (s Store) LockBuild(ctx context.Context, t source.Target) (source.Lease, error) {
	tx, e := s.repo().BeginFenced(ctx, t.Position)
	if e != nil {
		return nil, e
	}
	state, _, e := s.state(ctx, tx, t)
	if e != nil {
		rollback(ctx, tx)
		return nil, e
	}
	return &buildLease{state, tx}, nil
}
