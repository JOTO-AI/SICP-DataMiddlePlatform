package control

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// CompleteUnchanged never changes active metadata or published_at. All source
// progress, query checked-at metadata and task success commit together.
func (r Repository) CompleteUnchanged(ctx context.Context, expected ActiveMetadata, copies [2]SlotIdentity, at time.Time) error {
	if r.owner == nil {
		return ErrOwnership
	}
	if expected.Validate() != nil || at.IsZero() {
		return ErrEntity
	}
	at = at.UTC().Truncate(time.Microsecond)
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		plan, err := loadProgressPlan(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		if plan.Mode != "incremental" || plan.BundleID != expected.Position.BundleID || task.Mode != plan.Mode || task.StartedAt == nil || !task.StartedAt.Equal(plan.StartedAt) || task.PublishedAt != nil || at.Before(plan.CheckedAt) {
			return ErrEntity
		}
		if task.Status == "success_unchanged" {
			return nil
		}
		if task.Status != "running" {
			return ErrEntity
		}
		var pending bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		var raw *string
		if err = tx.QueryRow(ctx, "SELECT (SELECT target::text FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence DESC LIMIT 1)").Scan(&raw); err != nil {
			return err
		}
		if raw == nil {
			return ErrEntity
		}
		active, err := DecodeActive(Document(*raw))
		if err != nil || active != expected {
			return ErrIdempotency
		}
		for n, h := range copies {
			instance := storage.Main
			if n == 1 {
				instance = storage.Standby
			}
			if !h.valid() || h.Generation < 1 || h.Position.Instance != instance || h.BundleID != plan.BundleID || (instance == expected.Position.Position.Instance && h != expected.Position) {
				return ErrEntity
			}
			if err = publicationTarget(ctx, tx, PublicationIntent{Kind: "route_shift", Target: h}, r.owner.TaskID); err != nil {
				return err
			}
		}
		for _, table := range source.Catalog() {
			if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.source_progress (source,confirmed_until,checked_at,task_id) VALUES ($1,$2,$3,$4) ON CONFLICT (source) DO UPDATE SET confirmed_until=EXCLUDED.confirmed_until,checked_at=EXCLUDED.checked_at,task_id=EXCLUDED.task_id WHERE control.source_progress.confirmed_until<=EXCLUDED.confirmed_until AND control.source_progress.checked_at<=EXCLUDED.checked_at", table.Name, plan.Ends[table.Name], plan.CheckedAt, plan.TaskID)); err != nil {
				return err
			}
			if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.bundle_checks (bundle_id,source,checked_at,task_id) VALUES ($1,$2,$3,$4) ON CONFLICT (bundle_id,source) DO UPDATE SET checked_at=EXCLUDED.checked_at,task_id=EXCLUDED.task_id WHERE control.bundle_checks.checked_at<=EXCLUDED.checked_at", plan.BundleID, table.Name, plan.CheckedAt, plan.TaskID)); err != nil {
				return err
			}
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.bundles SET checked_at=$2 WHERE id=$1 AND published_at IS NOT NULL AND (checked_at IS NULL OR checked_at<=$2)", plan.BundleID, plan.CheckedAt)); err != nil {
			return err
		}
		return exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET status='success_unchanged',checked_at=$2,finished_at=$3,summary=COALESCE($4::jsonb,summary) WHERE id=$1 AND status='running'", plan.TaskID, plan.CheckedAt, at, optionalSummary(plan.Summary)))
	})
}
