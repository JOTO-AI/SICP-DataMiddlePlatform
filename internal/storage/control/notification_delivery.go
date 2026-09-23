package control

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// DueNotifications includes expired claims. Attempts are never reset: raising the
// configured total budget is the only automatic recovery of exhausted rows.
func (r Repository) DueNotifications(ctx context.Context, now time.Time, limit, maxAttempts int) ([]Notification, error) {
	if now.IsZero() || limit < 1 || limit > 100 || maxAttempts < 1 || maxAttempts > 16 {
		return nil, ErrEntity
	}
	var result []Notification
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrEntity
		}
		rows, err := q.Query(ctx, "SELECT id,failure_key,task_id,payload::text,status,attempts,created_at,next_attempt_at,sent_at FROM control.notification_outbox WHERE status IN ('pending','sending') AND attempts<$2 AND (next_attempt_at IS NULL OR next_attempt_at<=$3) ORDER BY created_at,id LIMIT $1", limit, maxAttempts, now.UTC().Truncate(time.Microsecond))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if len(result) >= limit {
				return ErrEntity
			}
			var n Notification
			if err := rows.Scan(&n.ID, &n.FailureKey, &n.TaskID, &n.Payload, &n.Status, &n.Attempts, &n.CreatedAt, &n.NextAttemptAt, &n.SentAt); err != nil {
				return err
			}
			if len(n.Payload) > 16384 {
				return ErrEntity
			}
			result = append(result, n)
		}
		return rows.Err()
	})
	return result, err
}

// AdvanceNotification atomically compares the observed revision under a row
// lock. A stale sender cannot complete a newer claim. No network call runs here.
func (r Repository) AdvanceNotification(ctx context.Context, before, after Notification) (bool, error) {
	if before.ID == "" || before.ID != after.ID || before.FailureKey != after.FailureKey || before.TaskID != after.TaskID || !before.CreatedAt.Equal(after.CreatedAt) || !sameNotice(before.Payload, after.Payload) || len(after.Payload) > 16384 || !json.Valid([]byte(after.Payload)) {
		return false, ErrEntity
	}
	claim := (before.Status == "pending" || before.Status == "sending") && after.Status == "sending" && after.Attempts == before.Attempts+1 && after.Attempts <= 16 && after.NextAttemptAt != nil && after.SentAt == nil
	finish := before.Status == "sending" && after.Attempts == before.Attempts && ((after.Status == "pending" && after.SentAt == nil && after.NextAttemptAt != nil) || ((after.Status == "accepted" || after.Status == "delivered") && after.SentAt != nil && after.NextAttemptAt == nil))
	if !claim && !finish {
		return false, ErrEntity
	}
	if err := r.validateWriter(&after); err != nil {
		return false, err
	}
	normalize := func(v *time.Time) *time.Time {
		if v == nil {
			return nil
		}
		t := v.UTC().Truncate(time.Microsecond)
		return &t
	}
	after.NextAttemptAt = normalize(after.NextAttemptAt)
	after.SentAt = normalize(after.SentAt)
	changed := false
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		d, _ := describe(&Notification{})
		found, err := readEntity(ctx, tx, d, `"id"=$1`, []any{before.ID}, true)
		if err != nil {
			return err
		}
		current := found.Interface().(Notification)
		// jsonb whitespace/key order is immaterial to revision comparison.
		var a, b any
		if json.Unmarshal([]byte(current.Payload), &a) != nil || json.Unmarshal([]byte(before.Payload), &b) != nil {
			return ErrEntity
		}
		if current.Attempts != before.Attempts || current.Status != before.Status || !reflect.DeepEqual(a, b) || !equalNotificationTime(current.NextAttemptAt, before.NextAttemptAt) || !equalNotificationTime(current.SentAt, before.SentAt) {
			return nil
		}
		count, err := tx.Exec(ctx, "UPDATE control.notification_outbox SET payload=$2::jsonb,status=$3,attempts=$4,next_attempt_at=$5,sent_at=$6 WHERE id=$1", after.ID, string(after.Payload), after.Status, after.Attempts, after.NextAttemptAt, after.SentAt)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrEntity
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}
func equalNotificationTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
