package control

import (
	"context"
	"math"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func (r Repository) repairCurrent(ctx context.Context, tx storage.Transaction, active ActiveMetadata, p storage.Position) error {
	if r.owner == nil {
		return ErrOwnership
	}
	if active.Validate() != nil || p.Validate() != nil || active.Position.Position.Instance == p.Instance {
		return ErrEntity
	}
	if err := r.fenceWriter(ctx, tx); err != nil {
		return err
	}
	var pending bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrBusy
	}
	var raw *string
	if err := tx.QueryRow(ctx, "SELECT (SELECT target::text FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence DESC LIMIT 1)").Scan(&raw); err != nil {
		return err
	}
	if raw == nil {
		return ErrEntity
	}
	confirmed, err := DecodeActive(Document(*raw))
	if err != nil || confirmed != active {
		return ErrIdempotency
	}
	return lockSlot(ctx, tx, p)
}
func (r Repository) IsolateRepair(ctx context.Context, active ActiveMetadata, h SlotIdentity) error {
	if !h.valid() || h.BundleID != active.Position.BundleID {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.repairCurrent(ctx, tx, active, h.Position); err != nil {
			return err
		}
		if _, err := loadSlot(ctx, tx, h); err != nil {
			return err
		}
		return exactlyOne(tx.Exec(ctx, "UPDATE control.slots SET admission_open=FALSE WHERE instance=$1 AND slot=$2", string(h.Position.Instance), int16(h.Position.Slot)))
	})
}
func (r Repository) PlanRepair(ctx context.Context, active ActiveMetadata, h SlotIdentity, id string) (source.Target, error) {
	if !h.valid() || h.Generation == math.MaxInt64 || h.BundleID != active.Position.BundleID || strings.TrimSpace(id) == "" || len(id) > 128 {
		return source.Target{}, ErrEntity
	}
	var out source.Target
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.repairCurrent(ctx, tx, active, h.Position); err != nil {
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
			if c.TaskID != r.owner.TaskID || c.OwnerGeneration != r.owner.Generation || c.Instance != string(h.Position.Instance) || int(c.Slot) != h.Position.Slot || c.Generation != h.Generation+1 || c.BundleID != h.BundleID {
				return ErrIdempotency
			}
			if _, err = loadSlot(ctx, tx, SlotIdentity{Position: h.Position, Generation: c.Generation, BundleID: h.BundleID}); err != nil {
				return err
			}
			out = source.Target{Position: h.Position, Generation: c.Generation, CandidateID: id}
			return nil
		}
		s, err := loadSlot(ctx, tx, h)
		if err != nil {
			return err
		}
		if s.AdmissionOpen || (s.State != "ready" && s.State != "failed") {
			return ErrBusy
		}
		var busy bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.readers WHERE instance=$1 AND slot=$2 AND (released_at IS NULL OR sql_finished_at IS NULL))", s.Instance, s.Slot).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrBusy
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.slots SET admission_open=FALSE,state='building',generation=$3 WHERE instance=$1 AND slot=$2", s.Instance, s.Slot, s.Generation+1)); err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.candidates (id,task_id,instance,slot,generation,bundle_id,owner_generation,state) VALUES ($1,$2,$3,$4,$5,$6,$7,'building')", id, r.owner.TaskID, s.Instance, s.Slot, s.Generation+1, h.BundleID, r.owner.Generation)); err != nil {
			return err
		}
		out = source.Target{Position: h.Position, Generation: s.Generation + 1, CandidateID: id}
		return nil
	})
	if err != nil {
		return source.Target{}, err
	}
	return out, nil
}
func (r Repository) OpenRepaired(ctx context.Context, active ActiveMetadata, target source.Target, code string) error {
	if target.Generation < 1 || target.CandidateID == "" || code == "" {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.repairCurrent(ctx, tx, active, target.Position); err != nil {
			return err
		}
		s, err := loadSlot(ctx, tx, SlotIdentity{Position: target.Position, Generation: target.Generation, BundleID: active.Position.BundleID})
		if err != nil {
			return err
		}
		if s.State != "ready" {
			return ErrBusy
		}
		var ready bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.candidates WHERE id=$1 AND task_id=$2 AND instance=$3 AND slot=$4 AND generation=$5 AND bundle_id=$6 AND owner_generation=$7 AND state='ready' AND ready_at IS NOT NULL AND evidence->>'CodeVersion'=$8)", target.CandidateID, r.owner.TaskID, s.Instance, s.Slot, target.Generation, active.Position.BundleID, r.owner.Generation, code).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return ErrBusy
		}
		return exactlyOne(tx.Exec(ctx, "UPDATE control.slots SET admission_open=TRUE WHERE instance=$1 AND slot=$2", s.Instance, s.Slot))
	})
}
