package runtimecontrol

import (
	"context"
	"encoding/json"
	"math"
	"reflect"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type copyPlan struct {
	Task     update.InitialTask
	Prepared *update.PreparedUpdate
	Targets  [2]source.Target
	Previous [2]control.Slot
	Old      update.Copy
}

func (s Store) nextTarget(ctx context.Context, tx storage.Transaction, p storage.Position) (source.Target, control.Slot, error) {
	v, e := slot(ctx, tx, p)
	if e != nil {
		return source.Target{}, v, e
	}
	var generation int64
	if e = tx.QueryRow(ctx, "SELECT GREATEST($3,COALESCE(MAX(generation),0)) FROM control.candidates WHERE instance=$1 AND slot=$2", string(p.Instance), int16(p.Slot), v.Generation).Scan(&generation); e != nil {
		return source.Target{}, v, e
	}
	if generation == math.MaxInt64 {
		return source.Target{}, v, control.ErrEntity
	}
	return source.Target{Position: p, Generation: generation + 1, CandidateID: s.Owner.TaskID + "-" + string(p.Instance)}, v, nil
}
func (s Store) PlanInitial(ctx context.Context, t update.InitialTask) (out [2]source.Target, err error) {
	err = s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		task, e := s.task(ctx, tx)
		if e != nil {
			return e
		}
		if task.ID != t.ID || task.Mode != "initialize" || !task.StartedAt.Equal(t.StartedAt) || t.CodeVersion != s.CodeVersion {
			return control.ErrEntity
		}
		if e = noPending(ctx, tx); e != nil {
			return e
		}
		a, e := current(ctx, tx)
		if e != nil {
			return e
		}
		if a != nil {
			return control.ErrAlreadyInitialized
		}
		var p copyPlan
		found, e := s.checkpoint(ctx, tx, "copy-plan", &p)
		if e != nil {
			return e
		}
		if found {
			if !reflect.DeepEqual(p.Task, t) {
				return control.ErrIdempotency
			}
			out = p.Targets
			return nil
		}
		p.Task = t
		for n, i := range []storage.Instance{storage.Main, storage.Standby} {
			p.Targets[n], p.Previous[n], e = s.nextTarget(ctx, tx, storage.Position{Instance: i, Slot: 0})
			if e != nil {
				return e
			}
		}
		out = p.Targets
		return s.save(ctx, tx, "copy-plan", p)
	})
	if err != nil {
		return [2]source.Target{}, err
	}
	return
}
func (s Store) RecordTarget(ctx context.Context, t update.InitialTask, targets [2]source.Target, m candidate.Manifest) error {
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		if _, e := s.task(ctx, tx); e != nil {
			return e
		}
		if e := noPending(ctx, tx); e != nil {
			return e
		}
		a, e := current(ctx, tx)
		if e != nil {
			return e
		}
		if a != nil {
			return control.ErrAlreadyInitialized
		}
		var p copyPlan
		found, e := s.checkpoint(ctx, tx, "copy-plan", &p)
		if e != nil {
			return e
		}
		if !found || !reflect.DeepEqual(p.Task, t) || p.Targets != targets || m.Combination != t.Combination {
			return control.ErrEntity
		}
		if e = s.saveBundle(ctx, tx, m); e != nil {
			return e
		}
		for _, target := range targets {
			if e = s.register(ctx, tx, target, m); e != nil {
				return e
			}
		}
		return nil
	})
}
func publishedCopy(ctx context.Context, tx storage.Transaction, bundleID string, i storage.Instance, code string) (out update.Copy, err error) {
	var id *string
	err = tx.QueryRow(ctx, "SELECT (SELECT c.id FROM control.candidates c JOIN control.slots s ON s.instance=c.instance AND s.slot=c.slot AND s.generation=c.generation AND s.bundle_id=c.bundle_id JOIN control.bundles b ON b.id=c.bundle_id WHERE c.instance=$1 AND c.bundle_id=$2 AND c.state='ready' AND s.state='ready' AND b.published_at IS NOT NULL ORDER BY c.generation DESC LIMIT 1)", string(i), bundleID).Scan(&id)
	if err != nil {
		return
	}
	if id == nil {
		return out, update.ErrOldCopy
	}
	c := control.Candidate{ID: *id}
	if err = control.ReadFact(ctx, tx, &c); err != nil {
		return
	}
	out.Target = source.Target{Position: storage.Position{Instance: i, Slot: int(c.Slot)}, Generation: c.Generation, CandidateID: c.ID}
	if out.Manifest, err = bundle(ctx, tx, bundleID); err != nil {
		return
	}
	if c.Evidence == nil || json.Unmarshal([]byte(*c.Evidence), &out.Evidence) != nil || out.Evidence.Validate(out.Target, out.Manifest, code) != nil {
		return update.Copy{}, update.ErrOldCopy
	}
	return
}
func (s Store) LoadPublished(ctx context.Context, p control.UpdatePlan) (out [2]update.Copy, err error) {
	err = s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		if p.Validate() != nil || p.TaskID != s.Owner.TaskID {
			return control.ErrEntity
		}
		if e := noPending(ctx, tx); e != nil {
			return e
		}
		for n, i := range []storage.Instance{storage.Main, storage.Standby} {
			v, e := publishedCopy(ctx, tx, p.Origin.Position.BundleID, i, s.CodeVersion)
			if e != nil {
				return e
			}
			out[n] = v
		}
		return nil
	})
	if err != nil {
		return [2]update.Copy{}, err
	}
	return
}
func (s Store) PlanCopies(ctx context.Context, in update.PreparedUpdate) (out [2]source.Target, old update.Copy, err error) {
	if in.Plan.Validate() != nil || in.Manifest.Validate() != nil || in.Plan.TaskID != s.Owner.TaskID || in.Manifest.Combination == in.Plan.Origin.Position.BundleID {
		return out, old, control.ErrEntity
	}
	in.Lease = nil // process-local lease is never persisted.
	err = s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		task, e := s.task(ctx, tx)
		if e != nil {
			return e
		}
		if task.Roles == nil {
			return control.ErrEntity
		}
		var roles control.UpdatePlan
		if json.Unmarshal([]byte(*task.Roles), &roles) != nil || !reflect.DeepEqual(roles, in.Plan) {
			return control.ErrEntity
		}
		if e = noPending(ctx, tx); e != nil {
			return e
		}
		var p copyPlan
		found, e := s.checkpoint(ctx, tx, "copy-plan", &p)
		if e != nil {
			return e
		}
		if found {
			if p.Prepared == nil || !reflect.DeepEqual(*p.Prepared, in) {
				return control.ErrIdempotency
			}
			out, old = p.Targets, p.Old
			return nil
		}
		a, e := current(ctx, tx)
		if e != nil {
			return e
		}
		if a == nil || *a != in.Plan.Origin {
			return control.ErrEntity
		}
		p.Prepared = &in
		for n, i := range []storage.Instance{in.Plan.First, in.Plan.Second} {
			copy, e := publishedCopy(ctx, tx, in.Plan.Origin.Position.BundleID, i, s.CodeVersion)
			if e != nil {
				return e
			}
			if n == 0 {
				p.Old = copy
			}
			position := storage.Position{Instance: i, Slot: 1 - copy.Target.Position.Slot}
			p.Targets[n], p.Previous[n], e = s.nextTarget(ctx, tx, position)
			if e != nil {
				return e
			}
		}
		if e = s.saveBundle(ctx, tx, in.Manifest); e != nil {
			return e
		}
		for _, target := range p.Targets {
			if e = s.register(ctx, tx, target, in.Manifest); e != nil {
				return e
			}
		}
		if e = s.save(ctx, tx, "copy-plan", p); e != nil {
			return e
		}
		out, old = p.Targets, p.Old
		return nil
	})
	if err != nil {
		return [2]source.Target{}, update.Copy{}, err
	}
	return
}
func (s Store) ConfirmCopies(ctx context.Context, p control.UpdatePlan, copies [2]update.Copy) error {
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		if p.TaskID != s.Owner.TaskID || p.Validate() != nil {
			return control.ErrEntity
		}
		for _, copy := range copies {
			c, e := s.candidate(ctx, tx, copy.Target)
			if e != nil {
				return e
			}
			sl, e := slot(ctx, tx, copy.Target.Position)
			if e != nil {
				return e
			}
			if c.State != "ready" || c.ReadyAt == nil || sl.Generation != c.Generation || sl.State != "ready" || sl.BundleID == nil || *sl.BundleID != copy.Manifest.Combination || c.Evidence == nil {
				return control.ErrEntity
			}
			var evidence candidate.Evidence
			if json.Unmarshal([]byte(*c.Evidence), &evidence) != nil || !reflect.DeepEqual(evidence, copy.Evidence) || evidence.Validate(copy.Target, copy.Manifest, s.CodeVersion) != nil {
				return control.ErrEntity
			}
		}
		return nil
	})
}
