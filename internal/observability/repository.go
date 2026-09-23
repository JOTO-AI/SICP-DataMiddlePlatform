package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type Repository struct {
	Registry storage.Registry
	Database storage.Database
}

// StoreBatch acknowledges only a committed independent logs transaction. The
// fixed event key makes an unknown commit outcome safe to replay.
func (r Repository) StoreBatch(ctx context.Context, batch []SafeEvent) error {
	if r.Database == nil || r.Registry.Logs().DSN() == "" || len(batch) < 1 || len(batch) > 1000 {
		return ErrDelivery
	}
	for _, e := range batch {
		if !e.Valid() {
			return ErrPayload
		}
	}
	tx, err := r.Database.Begin(ctx, r.Registry.Logs())
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
		count, err := tx.Exec(ctx, "INSERT INTO logs.events (event_id,kind,entity_id,phase,occurred_at,payload) VALUES ($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT (event_id) DO NOTHING", safe.ID(), string(e.Kind), e.EntityID, string(e.Phase), e.OccurredAt, string(safe.Bytes()))
		if err != nil || count < 0 || count > 1 {
			return ErrDelivery
		}
		if count == 0 {
			var stored string
			if tx.QueryRow(ctx, "SELECT payload::text FROM logs.events WHERE event_id=$1", safe.ID()).Scan(&stored) != nil {
				return ErrDelivery
			}
			if !sameJSON([]byte(stored), safe.Bytes()) {
				return ErrConflict
			}
		}
	}
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return ErrDelivery
	}
	return nil
}
func sameJSON(a, b []byte) bool {
	decode := func(raw []byte) []byte {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return nil
		}
		result, _ := json.Marshal(value)
		return result
	}
	left, right := decode(a), decode(b)
	return left != nil && right != nil && bytes.Equal(left, right)
}
