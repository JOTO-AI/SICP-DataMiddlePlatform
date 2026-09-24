package control

import (
	"context"
	"math"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func bundleReferenced(ctx context.Context, tx storage.Transaction, bundle string) error {
	var busy bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.resource_refs f JOIN control.resources r ON r.id=f.resource_id WHERE r.bundle_id=$1 AND f.released=FALSE)", bundle).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrBusy
	}
	return nil
}

// ReuseSlot reserves one exact old identity for the next bundle. This is the
// cross-round path: control current-route, explicit refs, snapshot retention and
// ALL target SQL are checked under common locks before generation advances.
func (r Repository) ReuseSlot(ctx context.Context, active ActiveMetadata, old SlotIdentity, bundle, id string) (source.Target, error) {
	if r.owner == nil || !old.valid() || old.Generation == math.MaxInt64 || strings.TrimSpace(bundle) == "" || bundle == old.BundleID || bundle == active.Position.BundleID || strings.TrimSpace(id) == "" || len(id) > 128 {
		return source.Target{}, ErrEntity
	}
	var out source.Target
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		if err := lockSnapshots(ctx, tx); err != nil {
			return err
		}
		if err := r.repairCurrent(ctx, tx, active, old.Position); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.candidates WHERE id=$1)", id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			d, _ := describe(&Candidate{})
			v, err := readEntity(ctx, tx, d, `"id"=$1`, []any{id}, true)
			if err != nil {
				return err
			}
			c := v.Interface().(Candidate)
			if c.TaskID != r.owner.TaskID || c.OwnerGeneration != r.owner.Generation || c.Instance != string(old.Position.Instance) || int(c.Slot) != old.Position.Slot || c.Generation != old.Generation+1 || c.BundleID != bundle {
				return ErrIdempotency
			}
			if _, err = loadSlot(ctx, tx, SlotIdentity{Position: old.Position, Generation: c.Generation, BundleID: bundle}); err != nil {
				return err
			}
			out = source.Target{Position: old.Position, Generation: c.Generation, CandidateID: id}
			return nil
		}
		s, err := loadSlot(ctx, tx, old)
		if err != nil {
			return err
		}
		if s.State != "ready" && s.State != "failed" && s.State != "empty" {
			return ErrBusy
		}
		if err = bundleReferenced(ctx, tx, old.BundleID); err != nil {
			return err
		}
		var busy bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.readers WHERE instance=$1 AND slot=$2 AND (released_at IS NULL OR sql_finished_at IS NULL)) OR EXISTS (SELECT 1 FROM control.query_snapshots WHERE bundle_id=$3 AND expires_at>CURRENT_TIMESTAMP)", s.Instance, s.Slot, old.BundleID).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrBusy
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.slots SET admission_open=FALSE,state='building',generation=$3,bundle_id=$4 WHERE instance=$1 AND slot=$2", s.Instance, s.Slot, s.Generation+1, bundle)); err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.candidates (id,task_id,instance,slot,generation,bundle_id,owner_generation,state) VALUES ($1,$2,$3,$4,$5,$6,$7,'building')", id, r.owner.TaskID, s.Instance, s.Slot, s.Generation+1, bundle, r.owner.Generation)); err != nil {
			return err
		}
		out = source.Target{Position: old.Position, Generation: s.Generation + 1, CandidateID: id}
		return nil
	})
	if err != nil {
		return source.Target{}, err
	}
	return out, nil
}
