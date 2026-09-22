package control

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func (r Repository) Cooldown(ctx context.Context) (time.Time, error) {
	var until *time.Time
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		return tx.QueryRow(ctx, "SELECT cooldown_until FROM control.schedule WHERE singleton=true").Scan(&until)
	})
	if err != nil {
		return time.Time{}, err
	}
	if until == nil {
		return time.Time{}, nil
	}
	return *until, nil
}

// ExtendCooldown is monotonic and independent of a task's lifetime. Concurrent
// or late responses cannot shorten a previously recorded upstream cooldown.
func (r Repository) ExtendCooldown(ctx context.Context, until time.Time) error {
	if until.IsZero() {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		n, err := tx.Exec(ctx, "UPDATE control.schedule SET cooldown_until=GREATEST(cooldown_until,$1) WHERE singleton=true", until)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrMissing
		}
		return nil
	})
}
