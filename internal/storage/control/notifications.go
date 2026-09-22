package control

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// EnqueueNotification persists one immutable final-failure notice in the fixed
// control database. Delivery metadata can evolve without reopening the failure.
func (r Repository) EnqueueNotification(ctx context.Context, n Notification) (Notification, bool, error) {
	if n.ID == "" || n.FailureKey == "" || strings.TrimSpace(n.TaskID) == "" || len(n.TaskID) > 128 || n.Status != "pending" || n.Attempts != 0 || n.CreatedAt.IsZero() || n.NextAttemptAt != nil || n.SentAt != nil || len(n.Payload) > 16384 || !json.Valid([]byte(n.Payload)) {
		return Notification{}, false, ErrEntity
	}
	if err := r.validateWriter(&n); err != nil {
		return Notification{}, false, err
	}
	// PostgreSQL timestamptz stores microseconds; freeze the database column at
	// that precision before INSERT/readback/replay comparisons.
	n.CreatedAt = n.CreatedAt.UTC().Truncate(time.Microsecond)
	var result Notification
	created := false
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		count, err := tx.Exec(ctx, "INSERT INTO control.notification_outbox (id,failure_key,task_id,payload,status,attempts,created_at) VALUES ($1,$2,$3,$4::jsonb,'pending',0,$5) ON CONFLICT (failure_key) DO NOTHING", n.ID, n.FailureKey, n.TaskID, string(n.Payload), n.CreatedAt)
		if err != nil {
			return err
		}
		if count < 0 || count > 1 {
			return ErrEntity
		}
		d, _ := describe(&Notification{})
		loaded, err := readEntity(ctx, tx, d, `"failure_key"=$1`, []any{n.FailureKey}, true)
		if err != nil {
			return err
		}
		existing := loaded.Interface().(Notification)
		if existing.ID != n.ID || existing.TaskID != n.TaskID || !existing.CreatedAt.Equal(n.CreatedAt) || !sameNotice(existing.Payload, n.Payload) {
			return ErrIdempotency
		}
		result = existing
		created = count == 1
		return nil
	})
	if err != nil {
		return Notification{}, false, err
	}
	return result, created, nil
}
func sameNotice(a, b Document) bool {
	notice := func(raw Document) []byte {
		var envelope struct {
			Notice json.RawMessage `json:"notice"`
		}
		if json.Unmarshal([]byte(raw), &envelope) != nil || len(envelope.Notice) == 0 {
			return nil
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(envelope.Notice))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return nil
		}
		canonical, _ := json.Marshal(value)
		return canonical
	}
	left, right := notice(a), notice(b)
	return left != nil && right != nil && bytes.Equal(left, right)
}

// PendingNotifications is a bounded recovery read, independent of the log PG.
func (r Repository) PendingNotifications(ctx context.Context, limit int) ([]Notification, error) {
	if limit < 1 || limit > 1000 {
		return nil, ErrEntity
	}
	var pending []Notification
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		query, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrEntity
		}
		rows, err := query.Query(ctx, "SELECT id,failure_key,task_id,payload::text,status,attempts,created_at,next_attempt_at,sent_at FROM control.notification_outbox WHERE status='pending' ORDER BY created_at,id LIMIT $1", limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if len(pending) >= limit {
				return ErrEntity
			}
			var n Notification
			if err := rows.Scan(&n.ID, &n.FailureKey, &n.TaskID, &n.Payload, &n.Status, &n.Attempts, &n.CreatedAt, &n.NextAttemptAt, &n.SentAt); err != nil {
				return err
			}
			if len(n.Payload) > 16384 {
				return ErrEntity
			}
			pending = append(pending, n)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return pending, nil
}
