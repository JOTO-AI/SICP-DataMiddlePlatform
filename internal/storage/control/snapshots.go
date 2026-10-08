package control

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type Snapshot struct {
	Token, BundleID, Operation, Conditions string
	CreatedAt, ExpiresAt                   time.Time
}

// Snapshot retention and last-copy replacement use this common persistent lock.
// P14 garbage collection must also take it before retiring retained bundles.
func lockSnapshots(ctx context.Context, tx storage.Transaction) error {
	defer buildmetric.LockAcquisition(ctx)()
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(734219003))
	return err
}

func (r Repository) PutSnapshot(ctx context.Context, s Snapshot) error {
	if len(s.Token) != 64 || s.BundleID == "" || s.Operation == "" || s.Conditions == "" || len(s.Conditions) > 512<<10 || s.CreatedAt.IsZero() || !s.ExpiresAt.After(s.CreatedAt) || s.ExpiresAt.Sub(s.CreatedAt) > time.Hour {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := lockSnapshots(ctx, tx); err != nil {
			return err
		}
		return exactlyOne(tx.Exec(ctx, "INSERT INTO control.query_snapshots (token,bundle_id,operation,conditions,created_at,expires_at) SELECT $1,$2,$3,$4,$5,$6 FROM control.bundles b WHERE b.id=$2 AND b.published_at IS NOT NULL AND EXISTS (SELECT 1 FROM control.slots s WHERE s.bundle_id=b.id AND s.state='ready' AND s.admission_open=TRUE)", s.Token, s.BundleID, s.Operation, s.Conditions, s.CreatedAt, s.ExpiresAt))
	})
}

func (r Repository) GetSnapshot(ctx context.Context, token string) (Snapshot, error) {
	if len(token) != 64 {
		return Snapshot{}, ErrEntity
	}
	var s Snapshot
	s.Token = token
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		return tx.QueryRow(ctx, "SELECT bundle_id,operation,conditions,created_at,expires_at FROM control.query_snapshots WHERE token=$1", token).Scan(&s.BundleID, &s.Operation, &s.Conditions, &s.CreatedAt, &s.ExpiresAt)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Snapshot{}, ErrMissing
		}
		return Snapshot{}, err
	}
	return s, nil
}
