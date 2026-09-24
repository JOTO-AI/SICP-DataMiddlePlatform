package adapters

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// QueryDatabase executes validation as the same database role used by public
// queries. Writer privileges must never mask a revoked query permission.
type QueryDatabase struct{ Database storage.Database }

func (d QueryDatabase) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, e := d.Database.Begin(ctx, c)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "SET LOCAL ROLE sicp_query"); e != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
		return nil, e
	}
	return tx, nil
}
