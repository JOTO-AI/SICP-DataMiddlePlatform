package control

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// DrainShift closes both slots of the original query instance only after a
// confirmed route shift. It counts every generation's request/actual SQL holds,
// not HTTP completion or lease expiry. Physical writers must still gate again.
func (r Repository) DrainShift(ctx context.Context, offered Publication) (bool, error) {
	if r.owner == nil {
		return false, ErrOwnership
	}
	drained := false
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		current, err := r.publicationForDelivery(ctx, tx, offered)
		if err != nil {
			return err
		}
		if current.Kind != "route_shift" || current.Status != "confirmed" || current.ConfirmedAt == nil || current.AppliedAt == nil || current.Expected == nil {
			return ErrEntity
		}
		origin, err := DecodeActive(*current.Expected)
		if err != nil {
			return err
		}
		target, err := DecodeActive(current.Target)
		if err != nil {
			return err
		}
		if origin.Position.Position.Instance == target.Position.Position.Instance || origin.Position.BundleID != target.Position.BundleID {
			return ErrEntity
		}
		var pending bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		var latest *string
		if err = tx.QueryRow(ctx, "SELECT (SELECT target::text FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence DESC LIMIT 1)").Scan(&latest); err != nil {
			return err
		}
		if latest == nil {
			return ErrEntity
		}
		active, err := DecodeActive(Document(*latest))
		if err != nil || active != target {
			return ErrIdempotency
		}
		for slot := 0; slot < 2; slot++ {
			if err = lockSlot(ctx, tx, storage.Position{Instance: origin.Position.Position.Instance, Slot: slot}); err != nil {
				return err
			}
		}
		count, err := tx.Exec(ctx, "UPDATE control.slots SET admission_open=FALSE WHERE instance=$1", string(origin.Position.Position.Instance))
		if err != nil {
			return err
		}
		if count != 2 {
			return ErrEntity
		}
		var busy bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.readers WHERE instance=$1 AND (released_at IS NULL OR sql_finished_at IS NULL))", string(origin.Position.Position.Instance)).Scan(&busy); err != nil {
			return err
		}
		drained = !busy
		return nil
	})
	if err != nil {
		return false, err
	}
	return drained, nil
}
