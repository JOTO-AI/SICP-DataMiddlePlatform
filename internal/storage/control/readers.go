package control

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// SlotIdentity is a complete physical generation, never just a live pointer.
type SlotIdentity struct {
	Position   storage.Position `json:"location"`
	Generation int64            `json:"generation,string"`
	BundleID   string           `json:"combination"`
}

func (h SlotIdentity) valid() bool {
	return h.Position.Validate() == nil && h.Generation >= 0 && len(h.BundleID) <= 256 && (h.Generation == 0 || strings.TrimSpace(h.BundleID) != "")
}

// All read-admission and replacement mutations take the same transaction lock
// in the fixed control database. Query roles need no UPDATE privilege on slots.
func lockSlot(ctx context.Context, tx storage.Transaction, p storage.Position) error {
	if p.Validate() != nil {
		return ErrEntity
	}
	index := p.Slot
	if p.Instance == storage.Standby {
		index += 2
	}
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", int32(734219002), int32(index))
	return err
}
func loadSlot(ctx context.Context, tx storage.Transaction, h SlotIdentity) (Slot, error) {
	var s Slot
	s.Instance = string(h.Position.Instance)
	s.Slot = int16(h.Position.Slot)
	err := tx.QueryRow(ctx, "SELECT generation,bundle_id,admission_open,state FROM control.slots WHERE instance=$1 AND slot=$2", s.Instance, s.Slot).Scan(&s.Generation, &s.BundleID, &s.AdmissionOpen, &s.State)
	if err != nil {
		return Slot{}, err
	}
	bundle := ""
	if s.BundleID != nil {
		bundle = *s.BundleID
	}
	if s.Generation != h.Generation || bundle != h.BundleID {
		return Slot{}, ErrEntity
	}
	return s, nil
}

func (r Repository) RegisterReader(ctx context.Context, h SlotIdentity, id, process string, at time.Time) (Reader, error) {
	if !h.valid() || h.Generation < 1 || strings.TrimSpace(id) == "" || strings.TrimSpace(process) == "" || at.IsZero() {
		return Reader{}, ErrEntity
	}
	reader := Reader{ID: id, Instance: string(h.Position.Instance), Slot: int16(h.Position.Slot), Generation: h.Generation, BundleID: h.BundleID, ProcessID: process, AdmittedAt: at, SQLFinishedAt: &at}
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := lockSlot(ctx, tx, h.Position); err != nil {
			return err
		}
		s, err := loadSlot(ctx, tx, h)
		if err != nil {
			return err
		}
		if !s.AdmissionOpen || s.State != "ready" {
			return ErrBusy
		}
		count, err := tx.Exec(ctx, "INSERT INTO control.readers (id,instance,slot,generation,bundle_id,process_id,admitted_at,sql_finished_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$7)", id, reader.Instance, reader.Slot, h.Generation, h.BundleID, process, at)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrEntity
		}
		return nil
	})
	if err != nil {
		return Reader{}, err
	}
	return reader, nil
}

func (r Repository) CloseAdmission(ctx context.Context, h SlotIdentity) error {
	if !h.valid() || r.owner == nil {
		return ErrOwnership
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		if err := lockSlot(ctx, tx, h.Position); err != nil {
			return err
		}
		if _, err := loadSlot(ctx, tx, h); err != nil {
			return err
		}
		count, err := tx.Exec(ctx, "UPDATE control.slots SET admission_open=FALSE WHERE instance=$1 AND slot=$2", string(h.Position.Instance), int16(h.Position.Slot))
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrEntity
		}
		return nil
	})
}

// ReserveOverwrite fences the next generation before any business-slot writes.
// The worker must separately establish that this is not the serving instance.
// Readers of ALL generations count: an old orphan must not be hidden by a newer
// pointer. HTTP release alone is insufficient if actual SQL is not finished.
func (r Repository) ReserveOverwrite(ctx context.Context, h SlotIdentity) (Slot, error) {
	if !h.valid() || h.Generation == math.MaxInt64 || r.owner == nil {
		return Slot{}, ErrOwnership
	}
	var next Slot
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		if err := lockSnapshots(ctx, tx); err != nil {
			return err
		}
		if err := lockSlot(ctx, tx, h.Position); err != nil {
			return err
		}
		s, err := loadSlot(ctx, tx, h)
		if err != nil {
			return err
		}
		if s.State != "ready" && s.State != "empty" && s.State != "failed" {
			return ErrBusy
		}
		var busy bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.readers WHERE instance=$1 AND slot=$2 AND (released_at IS NULL OR sql_finished_at IS NULL))", s.Instance, s.Slot).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrBusy
		}
		if err = bundleReferenced(ctx, tx, h.BundleID); err != nil {
			return err
		}
		var retained bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.query_snapshots q WHERE q.bundle_id=$1 AND q.expires_at>CURRENT_TIMESTAMP) AND NOT EXISTS (SELECT 1 FROM control.slots s WHERE s.bundle_id=$1 AND s.state='ready' AND s.admission_open=TRUE AND (s.instance<>$2 OR s.slot<>$3))", h.BundleID, s.Instance, s.Slot).Scan(&retained); err != nil {
			return err
		}
		if retained {
			return ErrBusy
		}
		count, err := tx.Exec(ctx, "UPDATE control.slots SET admission_open=FALSE,state='building',generation=$3,bundle_id=NULL WHERE instance=$1 AND slot=$2", s.Instance, s.Slot, s.Generation+1)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrEntity
		}
		next = Slot{Instance: s.Instance, Slot: s.Slot, Generation: s.Generation + 1, State: "building"}
		return nil
	})
	if err != nil {
		return Slot{}, err
	}
	return next, nil
}
