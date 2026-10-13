package observability

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type Repository struct {
	Registry     storage.Registry
	Database     storage.Database
	Source       storage.LogSource
	legacyReplay bool
}

// StoreBatch acknowledges only a committed independent logs transaction. The
// fixed event key makes an unknown commit outcome safe to replay.
func (r Repository) StoreBatch(ctx context.Context, batch []SafeEvent) error {
	if r.Database == nil || r.Registry.Logs().DSN() == "" || len(batch) < 1 || len(batch) > 1000 {
		return ErrDelivery
	}
	source := r.Source
	if source == "" {
		if r.Registry.LogMode() != storage.SingleLegacy {
			return ErrDelivery
		}
		source = storage.PlatformLog
	}
	connection, err := r.Registry.LogConnection(source)
	if err != nil {
		return ErrDelivery
	}
	for _, e := range batch {
		if !e.Valid() || (r.Registry.LogMode() == storage.DualLogs && !r.legacyReplay && !acceptsSource(source, e)) {
			return ErrPayload
		}
	}
	tx, err := r.Database.Begin(ctx, connection)
	if err != nil {
		return ErrDelivery
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	for _, safe := range batch {
		e := safe.Event()
		// Cutover runs after schema migration. Old buffered calls keep their
		// original target and payload, but must use the current writer protocol.
		var sequence int64
		// pgx truncates binary timestamps to microseconds, but PostgreSQL rounds
		// the JSON timestamp when append_event validates it. Parse both as text
		// on the server so nanosecond events (including old journal replays) agree.
		// Keep the immutable payload intact for receipt digest deduplication.
		if err := tx.QueryRow(ctx, "SELECT logs.append_event($1,$2,$3,$4,$5::text::timestamptz,$6::jsonb)", safe.ID(), string(e.Kind), e.EntityID, string(e.Phase), e.OccurredAt.Format(time.RFC3339Nano), string(safe.Bytes())).Scan(&sequence); err != nil {
			return ErrDelivery
		}
		if sequence == -1 {
			return ErrConflict
		}
		if sequence <= 0 {
			return ErrDelivery
		}
	}
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return ErrDelivery
	}
	return nil
}
