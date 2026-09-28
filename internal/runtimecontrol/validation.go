package runtimecontrol

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

func (s Store) BeginValidation(ctx context.Context, t source.Target, m candidate.Manifest) (out candidate.Evidence, err error) {
	err = s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		_, c, e := s.state(ctx, tx, t)
		if e != nil {
			return e
		}
		if c.State != "storage_complete" && c.State != "ready" && c.State != "validating" {
			return control.ErrEntity
		}
		actual, e := bundle(ctx, tx, c.BundleID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(actual, m) {
			return control.ErrEntity
		}
		if c.Evidence != nil {
			_ = json.Unmarshal([]byte(*c.Evidence), &out)
		}
		if e = one(tx.Exec(ctx, "UPDATE control.candidates SET state='validating',ready_at=NULL WHERE id=$1", t.CandidateID)); e != nil {
			return e
		}
		return one(tx.Exec(ctx, "UPDATE control.slots SET state='validating',admission_open=FALSE WHERE instance=$1 AND slot=$2", c.Instance, c.Slot))
	})
	if err != nil {
		return candidate.Evidence{}, err
	}
	return
}
func (s Store) Ready(ctx context.Context, t source.Target, m candidate.Manifest, ev candidate.Evidence) error {
	if ev.Validate(t, m) != nil {
		return control.ErrEntity
	}
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		_, c, e := s.state(ctx, tx, t)
		if e != nil {
			return e
		}
		if c.State != "validating" {
			return control.ErrEntity
		}
		actual, e := bundle(ctx, tx, c.BundleID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(actual, m) {
			return control.ErrEntity
		}
		task, e := s.task(ctx, tx)
		if e != nil {
			return e
		}
		if ev.CheckedAt.Before(*task.StartedAt) {
			return control.ErrTime
		}
		if e = one(tx.Exec(ctx, "UPDATE control.candidates SET state='ready',ready_at=$2,evidence=$3::jsonb WHERE id=$1", t.CandidateID, ev.CheckedAt, document(ev))); e != nil {
			return e
		}
		if e = one(tx.Exec(ctx, "UPDATE control.slots SET state='ready' WHERE instance=$1 AND slot=$2", c.Instance, c.Slot)); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE control.tasks SET ready_at=$2 WHERE id=$1 AND ready_at IS NULL AND (SELECT COUNT(DISTINCT c.instance) FROM control.candidates c JOIN control.slots s ON s.instance=c.instance AND s.slot=c.slot AND s.generation=c.generation AND s.bundle_id=c.bundle_id WHERE c.task_id=$1 AND c.bundle_id=$3 AND c.state='ready' AND s.state='ready')=2", s.Owner.TaskID, ev.CheckedAt, m.Combination)
		return e
	})
}
func (s Store) ValidationFailed(ctx context.Context, t source.Target) error { return s.Fail(ctx, t) }

// Candidate reads hold the same fenced slot locks through the physical read;
// closed unpublished candidates are never registered as public query readers.
type validationLease struct {
	h  read.Handle
	tx storage.Transaction
}

func (l *validationLease) Handle() read.Handle { return l.h }
func (l *validationLease) Release(ctx context.Context) error {
	defer rollback(ctx, l.tx)
	return l.tx.Commit(ctx)
}
func (s Store) BeginRead(ctx context.Context, h read.Handle) (read.SQLLease, error) {
	if h.Validate() != nil {
		return nil, read.ErrHandle
	}
	tx, e := s.repo().BeginFenced(ctx, h.Position)
	if e != nil {
		return nil, e
	}
	var id *string
	e = tx.QueryRow(ctx, "SELECT (SELECT id FROM control.candidates WHERE instance=$1 AND slot=$2 AND generation=$3 AND bundle_id=$4 AND task_id=$5 AND state='validating')", string(h.Position.Instance), int16(h.Position.Slot), h.Generation, h.Combination, s.Owner.TaskID).Scan(&id)
	if e != nil || id == nil {
		rollback(ctx, tx)
		if e != nil {
			return nil, e
		}
		return nil, read.ErrHandle
	}
	_, _, e = s.state(ctx, tx, source.Target{Position: h.Position, Generation: h.Generation, CandidateID: *id})
	if e != nil {
		rollback(ctx, tx)
		return nil, e
	}
	return &validationLease{h, tx}, nil
}

func (s Store) proof(ctx context.Context, tx storage.Transaction, p update.ReadabilityProof) error {
	t, e := s.task(ctx, tx)
	if e != nil {
		return e
	}
	if p.TaskID != s.Owner.TaskID || p.CheckedAt.Before(*t.StartedAt) {
		return control.ErrEntity
	}
	if e = noPending(ctx, tx); e != nil {
		return e
	}
	a, e := current(ctx, tx)
	if e != nil {
		return e
	}
	if a == nil || *a != p.Origin {
		return control.ErrEntity
	}
	c, e := s.candidate(ctx, tx, p.Candidate)
	if e != nil {
		return e
	}
	v, e := slot(ctx, tx, p.Candidate.Position)
	if e != nil {
		return e
	}
	if c.BundleID != p.Combination || c.State != "ready" || c.Evidence == nil || v.Generation != c.Generation || v.BundleID == nil || *v.BundleID != c.BundleID || v.State != "ready" || v.AdmissionOpen {
		return control.ErrEntity
	}
	m, e := bundle(ctx, tx, c.BundleID)
	if e != nil {
		return e
	}
	var evidence candidate.Evidence
	if json.Unmarshal([]byte(*c.Evidence), &evidence) != nil || evidence.Validate(p.Candidate, m) != nil {
		return control.ErrEntity
	}
	old, e := publishedCopy(ctx, tx, p.Origin.Position.BundleID, p.Destination.Position.Instance)
	if e != nil {
		return e
	}
	if old.Target.Position != p.Destination.Position || old.Target.Generation != p.Destination.Generation || p.Destination.BundleID != p.Origin.Position.BundleID || old.Target.Position == p.Candidate.Position || p.Candidate.Position.Instance == p.Origin.Position.Position.Instance || old.Target.Position.Instance != p.Candidate.Position.Instance {
		return control.ErrEntity
	}
	v, e = slot(ctx, tx, old.Target.Position)
	if e != nil {
		return e
	}
	if !v.AdmissionOpen {
		return control.ErrBusy
	}
	return nil
}
func (s Store) RecordReadability(ctx context.Context, p update.ReadabilityProof, next, old update.Copy) error {
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		if next.Evidence.Validate(next.Target, next.Manifest) != nil || old.Evidence.Validate(old.Target, old.Manifest) != nil || next.Target != p.Candidate || next.Manifest.Combination != p.Combination || old.Target.Position != p.Destination.Position || old.Target.Generation != p.Destination.Generation || old.Manifest.Combination != p.Destination.BundleID {
			return control.ErrEntity
		}
		if e := s.proof(ctx, tx, p); e != nil {
			return e
		}
		return s.save(ctx, tx, "readability-proof", p)
	})
}
func (s Store) AuthorizeShift(ctx context.Context, p update.ReadabilityProof) error {
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		var saved update.ReadabilityProof
		found, e := s.checkpoint(ctx, tx, "readability-proof", &saved)
		if e != nil {
			return e
		}
		if !found || !reflect.DeepEqual(saved, p) {
			return control.ErrEntity
		}
		return s.proof(ctx, tx, p)
	})
}
