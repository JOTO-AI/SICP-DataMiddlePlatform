package control

import (
	"context"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type TaskSummary struct {
	ID         string    `json:"task_id"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status"`
	Operator   string    `json:"operator"`
	AcceptedAt time.Time `json:"accepted_at"`
}

type TaskOverview struct {
	Items           []TaskSummary `json:"items"`
	Initialized     bool          `json:"initialized"`
	NextDueAt       *time.Time    `json:"next_due_at"`
	CooldownUntil   *time.Time    `json:"cooldown_until"`
	IntervalSeconds int64         `json:"interval_seconds"`
}

// RecentTasks exposes a bounded, credential-free view of automatic and manual
// work. Existing task detail remains the source of progress and failure facts.
func (r Repository) RecentTasks(ctx context.Context) (TaskOverview, error) {
	out := TaskOverview{Items: []TaskSummary{}, IntervalSeconds: int64(SyncInterval / time.Second)}
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if _, err := readExecution(ctx, tx); err != nil {
			return err
		}
		s, err := loadSchedule(ctx, tx, false)
		if err != nil {
			return err
		}
		out.NextDueAt, out.CooldownUntil = s.NextDueAt, s.CooldownUntil
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='confirmed' AND kind='bundle_publish')").Scan(&out.Initialized); err != nil {
			return err
		}
		var raw string
		if err = tx.QueryRow(ctx, `SELECT COALESCE(json_agg(t ORDER BY accepted_at DESC, task_id DESC), '[]'::json)::text FROM
 (SELECT id AS task_id, mode, status, requested_by AS operator, accepted_at FROM control.tasks ORDER BY accepted_at DESC, id DESC LIMIT 50) t`).Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal([]byte(raw), &out.Items)
	})
	return out, err
}
