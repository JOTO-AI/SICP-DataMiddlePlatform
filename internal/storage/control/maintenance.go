package control

import (
	"context"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// MaintenanceBundles lists bounded published resources whose physical cleanup
// has not succeeded. This is discovery only; Plan/RetireCollection recheck all
// references under the execution, snapshot and physical slot locks.
func (r Repository) MaintenanceBundles(ctx context.Context, limit int) ([]Bundle, error) {
	if r.owner == nil || limit < 1 || limit > 100 {
		return nil, ErrEntity
	}
	var out []Bundle
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		query, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrEntity
		}
		rows, err := query.Query(ctx, `SELECT b.id,b.tables::text,b.created_at,b.published_at,b.checked_at
FROM control.bundles b JOIN control.resources r ON r.bundle_id=b.id AND r.id='version-'||b.id AND r.kind='version'
WHERE b.published_at IS NOT NULL
AND NOT EXISTS (SELECT 1 FROM control.steps WHERE name='maintenance/'||r.id AND status='succeeded')
AND NOT EXISTS (SELECT 1 FROM control.resource_refs WHERE resource_id=r.id AND released=FALSE)
AND NOT EXISTS (SELECT 1 FROM control.query_snapshots WHERE bundle_id=b.id AND expires_at>CURRENT_TIMESTAMP)
AND NOT EXISTS (SELECT 1 FROM control.readers WHERE bundle_id=b.id AND (released_at IS NULL OR sql_finished_at IS NULL))
AND b.id<>(SELECT target->'position'->>'combination' FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence DESC LIMIT 1)
ORDER BY b.created_at,b.id LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Bundle
			if len(out) >= limit {
				return ErrEntity
			}
			if err := rows.Scan(&b.ID, &b.Tables, &b.CreatedAt, &b.PublishedAt, &b.CheckedAt); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

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
