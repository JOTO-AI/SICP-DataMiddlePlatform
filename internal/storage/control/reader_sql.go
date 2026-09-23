package control

import (
	"context"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func loadReader(ctx context.Context, tx storage.Transaction, h SlotIdentity, id string) (Reader, error) {
	var r Reader
	r.ID = id
	err := tx.QueryRow(ctx, "SELECT instance,slot,generation,bundle_id,admitted_at,released_at,sql_finished_at FROM control.readers WHERE id=$1", id).Scan(&r.Instance, &r.Slot, &r.Generation, &r.BundleID, &r.AdmittedAt, &r.ReleasedAt, &r.SQLFinishedAt)
	if err != nil {
		return Reader{}, err
	}
	if r.Instance != string(h.Position.Instance) || int(r.Slot) != h.Position.Slot || r.Generation != h.Generation || r.BundleID != h.BundleID {
		return Reader{}, ErrEntity
	}
	return r, nil
}
func exactlyOne(count int64, err error) error {
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrEntity
	}
	return nil
}

// BeginSQL accepts previously registered requests after admission closes, but
// never a bare old pointer, a released request, or a replacement generation.
func (r Repository) BeginSQL(ctx context.Context, h SlotIdentity, readerID, executionID string, at time.Time) error {
	if !h.valid() || readerID == "" || strings.TrimSpace(executionID) == "" || at.IsZero() {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := lockSlot(ctx, tx, h.Position); err != nil {
			return err
		}
		s, err := loadSlot(ctx, tx, h)
		if err != nil {
			return err
		}
		if s.State != "ready" {
			return ErrBusy
		}
		reader, err := loadReader(ctx, tx, h, readerID)
		if err != nil {
			return err
		}
		if reader.ReleasedAt != nil {
			return ErrBusy
		}
		if at.Before(reader.AdmittedAt) || reader.SQLFinishedAt != nil && at.Before(*reader.SQLFinishedAt) {
			return ErrTime
		}
		if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.reader_sql (id,reader_id,started_at) VALUES ($1,$2,$3)", executionID, readerID, at)); err != nil {
			return err
		}
		return exactlyOne(tx.Exec(ctx, "UPDATE control.readers SET sql_finished_at=NULL WHERE id=$1", readerID))
	})
}

// FinishSQL is called only with actual execution termination evidence. A late
// duplicate completion cannot finish a newer execution for this reader.
func (r Repository) FinishSQL(ctx context.Context, h SlotIdentity, readerID, executionID string, at time.Time) error {
	if !h.valid() || readerID == "" || executionID == "" || at.IsZero() {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := lockSlot(ctx, tx, h.Position); err != nil {
			return err
		}
		if _, err := loadReader(ctx, tx, h, readerID); err != nil {
			return err
		}
		var started time.Time
		var finished *time.Time
		if err := tx.QueryRow(ctx, "SELECT started_at,finished_at FROM control.reader_sql WHERE id=$1 AND reader_id=$2", executionID, readerID).Scan(&started, &finished); err != nil {
			return err
		}
		if finished != nil {
			return nil
		}
		if at.Before(started) {
			return ErrTime
		}
		if err := exactlyOne(tx.Exec(ctx, "UPDATE control.reader_sql SET finished_at=$3 WHERE id=$1 AND reader_id=$2", executionID, readerID, at)); err != nil {
			return err
		}
		var active bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.reader_sql WHERE reader_id=$1 AND finished_at IS NULL)", readerID).Scan(&active); err != nil {
			return err
		}
		if active {
			return nil
		}
		return exactlyOne(tx.Exec(ctx, "UPDATE control.readers SET sql_finished_at=$2 WHERE id=$1", readerID, at))
	})
}

// ReleaseReader ends HTTP/cache use only. It never marks unfinished SQL done.
func (r Repository) ReleaseReader(ctx context.Context, h SlotIdentity, id string, at time.Time) error {
	if !h.valid() || id == "" || at.IsZero() {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := lockSlot(ctx, tx, h.Position); err != nil {
			return err
		}
		reader, err := loadReader(ctx, tx, h, id)
		if err != nil {
			return err
		}
		if reader.ReleasedAt != nil {
			return nil
		}
		if at.Before(reader.AdmittedAt) {
			return ErrTime
		}
		return exactlyOne(tx.Exec(ctx, "UPDATE control.readers SET released_at=$2 WHERE id=$1", id, at))
	})
}

func (r Repository) Drained(ctx context.Context, h SlotIdentity) (bool, error) {
	if !h.valid() {
		return false, ErrEntity
	}
	drained := false
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := lockSlot(ctx, tx, h.Position); err != nil {
			return err
		}
		s, err := loadSlot(ctx, tx, h)
		if err != nil {
			return err
		}
		if s.AdmissionOpen {
			return nil
		}
		var busy bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.readers WHERE instance=$1 AND slot=$2 AND (released_at IS NULL OR sql_finished_at IS NULL))", s.Instance, s.Slot).Scan(&busy); err != nil {
			return err
		}
		drained = !busy
		return nil
	})
	return drained && err == nil, err
}
