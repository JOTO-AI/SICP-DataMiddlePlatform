package control

import (
	"context"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// RecordMaintenance cannot update task status, published times or progress.
// Errors are represented by a bounded code, never raw filesystem/DSN payloads.
func (r Repository) RecordMaintenance(ctx context.Context, resource string, success bool, at time.Time) error {
	if r.owner == nil || strings.TrimSpace(resource) == "" || len(resource) > 256 || at.IsZero() {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		if (task.Status != "success_published" && task.Status != "success_unchanged") || task.FinishedAt == nil {
			return ErrEntity
		}
		if at.Before(*task.FinishedAt) {
			return ErrTime
		}
		status, evidence := "failed", `{"result":"cleanup_failed"}`
		if success {
			status = "succeeded"
			evidence = `{"result":"cleaned"}`
		}
		count, err := tx.Exec(ctx, "INSERT INTO control.steps (task_id,name,status,finished_at,evidence) VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (task_id,name) DO UPDATE SET status=EXCLUDED.status,finished_at=EXCLUDED.finished_at,evidence=EXCLUDED.evidence WHERE control.steps.status<>'succeeded'", task.ID, "maintenance/"+resource, status, at, evidence)
		if err != nil {
			return err
		}
		if count < 0 || count > 1 {
			return ErrEntity
		}
		return nil
	})
}
