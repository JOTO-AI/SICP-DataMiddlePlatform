package control

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const progressCheckpoint = "successful-progress-plan"
const IncrementalOverlap = 5 * time.Minute

// ProgressPlan is immutable complete-round evidence, staged before publishing.
// Ends are the acquisition's fixed upper bounds, never the publication time.
type ProgressPlan struct {
	TaskID, BundleID, Mode string
	StartedAt, CheckedAt   time.Time
	Ends                   map[string]time.Time
	Summary                Document
	Retained               bool
}

func (p ProgressPlan) valid() bool {
	if strings.TrimSpace(p.TaskID) == "" || strings.TrimSpace(p.BundleID) == "" || len(p.TaskID) > 128 || len(p.BundleID) > 256 || p.StartedAt.IsZero() || p.CheckedAt.Before(p.StartedAt) || len(p.Ends) != len(source.Catalog()) || !validSummary(p.Summary) {
		return false
	}
	if p.Mode != "initialize" && p.Mode != "force_full" && p.Mode != "incremental" {
		return false
	}
	for _, table := range source.Catalog() {
		if (!p.Retained && !p.Ends[table.Name].Equal(p.StartedAt)) || p.Ends[table.Name].IsZero() || p.Ends[table.Name].After(p.StartedAt) || (p.Retained && p.Mode == "incremental") {
			return false
		}
	}
	return true
}
func normalizePlan(p ProgressPlan) ProgressPlan {
	p.StartedAt = p.StartedAt.UTC().Truncate(time.Microsecond)
	p.CheckedAt = p.CheckedAt.UTC().Truncate(time.Microsecond)
	p.Ends = maps.Clone(p.Ends)
	for name, end := range p.Ends {
		p.Ends[name] = end.UTC().Truncate(time.Microsecond)
	}
	return p
}
func samePlan(a, b ProgressPlan) bool {
	return a.TaskID == b.TaskID && a.BundleID == b.BundleID && a.Mode == b.Mode && a.StartedAt.Equal(b.StartedAt) && a.CheckedAt.Equal(b.CheckedAt) && maps.EqualFunc(a.Ends, b.Ends, time.Time.Equal) && a.Summary == b.Summary && a.Retained == b.Retained
}
func loadTask(ctx context.Context, tx storage.Transaction, id string) (Task, error) {
	d, _ := describe(&Task{})
	loaded, err := readEntity(ctx, tx, d, `"id"=$1`, []any{id}, true)
	if err != nil {
		return Task{}, err
	}
	return loaded.Interface().(Task), nil
}
func loadProgressPlan(ctx context.Context, tx storage.Transaction, id string) (ProgressPlan, error) {
	var raw string
	if err := tx.QueryRow(ctx, "SELECT payload::text FROM control.checkpoints WHERE task_id=$1 AND name=$2", id, progressCheckpoint).Scan(&raw); err != nil {
		return ProgressPlan{}, err
	}
	var p ProgressPlan
	if json.Unmarshal([]byte(raw), &p) != nil || !p.valid() || p.TaskID != id {
		return ProgressPlan{}, ErrEntity
	}
	return p, nil
}

func (r Repository) StageProgress(ctx context.Context, p ProgressPlan) error {
	if r.owner == nil || p.TaskID != r.owner.TaskID {
		return ErrOwnership
	}
	if !p.valid() {
		return ErrEntity
	}
	p = normalizePlan(p)
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, p.TaskID)
		if err != nil {
			return err
		}
		if task.StartedAt == nil || !task.StartedAt.Equal(p.StartedAt) || task.Mode != p.Mode || task.Status != "running" || task.PublishedAt != nil {
			return ErrEntity
		}
		if p.Retained {
			if p.Mode == "initialize" {
				var at time.Time
				if err := tx.QueryRow(ctx, "SELECT (payload->>'at')::timestamptz FROM control.checkpoints WHERE task_id=$1 AND name='source-epoch'", task.ID).Scan(&at); err != nil {
					return err
				}
				for _, end := range p.Ends {
					if !end.Equal(at) {
						return ErrEntity
					}
				}
			} else {
				var plan UpdatePlan
				if task.Roles == nil || json.Unmarshal([]byte(*task.Roles), &plan) != nil || plan.Validate() != nil || (plan.Table == "" && !plan.Repair) || plan.TaskID != task.ID || plan.Mode != task.Mode || !plan.StartedAt.Equal(*task.StartedAt) {
					return ErrEntity
				}
				for name, end := range p.Ends {
					want := plan.StartedAt
					if plan.Repair || name != plan.Table {
						want = plan.Windows[name].ConfirmedUntil
					}
					if !end.Equal(want) {
						return ErrEntity
					}
				}
			}
		}
		count, err := tx.Exec(ctx, "INSERT INTO control.checkpoints (task_id,name,payload,recorded_at) VALUES ($1,$2,$3::jsonb,$4) ON CONFLICT (task_id,name) DO NOTHING", p.TaskID, progressCheckpoint, string(raw), p.CheckedAt)
		if err != nil {
			return err
		}
		if count < 0 || count > 1 {
			return ErrEntity
		}
		stored, err := loadProgressPlan(ctx, tx, p.TaskID)
		if err != nil {
			return err
		}
		if !samePlan(stored, p) {
			return ErrIdempotency
		}
		return nil
	})
}

// ConfirmPublication commits publication and admission independently of source
// progress. A prior acknowledged atomic application is mandatory; GET alone is
// never application evidence. Replays preserve the original publication time.
func (r Repository) ConfirmPublication(ctx context.Context, offered Publication, at time.Time) (result Publication, err error) {
	end := events.BeginBuild(ctx, events.BuildScope{Stage: "publish_confirm"}, "")
	defer func() { end(err) }()
	if r.owner == nil {
		return Publication{}, ErrOwnership
	}
	if at.IsZero() {
		return Publication{}, ErrTime
	}
	at = at.UTC().Truncate(time.Microsecond)
	var confirmed Publication
	err = r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		// Fixed lock order also protects opening the second copy against reserve.
		for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
			for slot := 0; slot < 2; slot++ {
				if err := lockSlot(ctx, tx, storage.Position{Instance: instance, Slot: slot}); err != nil {
					return err
				}
			}
		}
		current, err := r.publicationForDelivery(ctx, tx, offered)
		if err != nil {
			return err
		}
		if current.Status == "confirmed" {
			confirmed = current
			return nil
		}
		if current.AppliedAt == nil || at.Before(*current.AppliedAt) {
			return ErrTime
		}
		target, err := DecodeActive(current.Target)
		if err != nil {
			return err
		}
		if current.Kind == "bundle_publish" {
			plan, err := loadProgressPlan(ctx, tx, current.TaskID)
			if err != nil {
				return err
			}
			if plan.BundleID != target.Position.BundleID || plan.CheckedAt.After(*current.AppliedAt) {
				return ErrEntity
			}
			task, err := loadTask(ctx, tx, current.TaskID)
			if err != nil {
				return err
			}
			if task.StartedAt == nil || !task.StartedAt.Equal(plan.StartedAt) || task.Mode != plan.Mode || task.Status != "running" || task.PublishedAt != nil {
				return ErrEntity
			}
			if err = exactlyOne(tx.Exec(ctx, "UPDATE control.bundles SET published_at=$2 WHERE id=$1 AND published_at IS NULL", plan.BundleID, *current.AppliedAt)); err != nil {
				return err
			}
			// Read metadata is part of the confirmed published bundle. A later
			// source-progress failure must not make already published data unreadable.
			for _, table := range source.Catalog() {
				if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.bundle_checks (bundle_id,source,checked_at,task_id) VALUES ($1,$2,$3,$4) ON CONFLICT (bundle_id,source) DO UPDATE SET checked_at=EXCLUDED.checked_at,task_id=EXCLUDED.task_id WHERE control.bundle_checks.checked_at<=EXCLUDED.checked_at", plan.BundleID, table.Name, plan.CheckedAt, plan.TaskID)); err != nil {
					return err
				}
			}
			count, err := tx.Exec(ctx, "UPDATE control.slots s SET admission_open=TRUE FROM control.candidates c WHERE c.instance=s.instance AND c.slot=s.slot AND c.generation=s.generation AND c.bundle_id=s.bundle_id AND c.bundle_id=$1 AND c.task_id=$2 AND c.state='ready' AND c.ready_at IS NOT NULL AND c.evidence IS NOT NULL AND s.state='ready' AND NOT s.admission_open", plan.BundleID, current.TaskID)
			if err != nil {
				return err
			}
			if count != 2 {
				return ErrBusy
			}
			if err = exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET published_at=$2,status='published_pending_progress' WHERE id=$1 AND status='running' AND published_at IS NULL", current.TaskID, *current.AppliedAt)); err != nil {
				return err
			}
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.publication_outbox SET status='confirmed',confirmed_at=$2 WHERE id=$1 AND status='pending' AND applied_at IS NOT NULL", current.ID, at)); err != nil {
			return err
		}
		current.Status = "confirmed"
		current.ConfirmedAt = &at
		confirmed = current
		return nil
	})
	if err != nil {
		return Publication{}, err
	}
	return confirmed, nil
}

// CompletePublished advances all fourteen sources and task success in one
// transaction. A failed/unknown commit can be retried using
// only its persisted event/plan; no source acquisition or physical write occurs.
func (r Repository) CompletePublished(ctx context.Context, offered Publication, at time.Time) (err error) {
	end := events.BeginBuild(ctx, events.BuildScope{Stage: "confirm_progress"}, "")
	defer func() { end(err) }()
	if r.owner == nil {
		return ErrOwnership
	}
	if at.IsZero() {
		return ErrTime
	}
	at = at.UTC().Truncate(time.Microsecond)
	return r.transaction(ctx, func(tx storage.Transaction) error {
		current, err := r.publicationForDelivery(ctx, tx, offered)
		if err != nil {
			return err
		}
		if current.Kind != "bundle_publish" || current.Status != "confirmed" || current.ConfirmedAt == nil || current.AppliedAt == nil || at.Before(*current.ConfirmedAt) {
			return ErrEntity
		}
		target, err := DecodeActive(current.Target)
		if err != nil {
			return err
		}
		plan, err := loadProgressPlan(ctx, tx, current.TaskID)
		if err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, current.TaskID)
		if err != nil {
			return err
		}
		if plan.BundleID != target.Position.BundleID || task.StartedAt == nil || !task.StartedAt.Equal(plan.StartedAt) || task.Mode != plan.Mode || task.PublishedAt == nil || !task.PublishedAt.Equal(*current.AppliedAt) || plan.CheckedAt.After(*current.AppliedAt) {
			return ErrEntity
		}
		if task.Status == "success_published" {
			return nil
		}
		if task.Status != "published_pending_progress" {
			return ErrEntity
		}
		for _, table := range source.Catalog() {
			if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.source_progress (source,confirmed_until,checked_at,task_id) VALUES ($1,$2,$3,$4) ON CONFLICT (source) DO UPDATE SET confirmed_until=EXCLUDED.confirmed_until,checked_at=EXCLUDED.checked_at,task_id=EXCLUDED.task_id WHERE control.source_progress.confirmed_until<=EXCLUDED.confirmed_until AND control.source_progress.checked_at<=EXCLUDED.checked_at", table.Name, plan.Ends[table.Name], plan.CheckedAt, plan.TaskID)); err != nil {
				return err
			}
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.bundles SET checked_at=$2 WHERE id=$1 AND published_at IS NOT NULL AND (checked_at IS NULL OR checked_at<=$2)", plan.BundleID, plan.CheckedAt)); err != nil {
			return err
		}
		return exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET status='success_published',checked_at=$2,finished_at=$3,summary=COALESCE($4::jsonb,summary) WHERE id=$1 AND status='published_pending_progress'", plan.TaskID, plan.CheckedAt, at, optionalSummary(plan.Summary)))
	})
}
