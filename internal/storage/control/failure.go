package control

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// FailTask preserves unknown termination time as SQL NULL. Only an observed
// terminal outcome may supply a time; discovery of an interrupted run may not.
func (r Repository) FailTask(ctx context.Context, reason string, finished *time.Time) error {
	if r.owner == nil {
		return ErrOwnership
	}
	if reason != "acquisition_failed" && reason != "corrupt_seal" && reason != "interrupted" {
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
		if task.Status == "failed" {
			return nil
		}
		if task.Status != "running" || task.StartedAt == nil {
			return ErrEntity
		}
		var pending bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		if finished != nil && (finished.IsZero() || finished.Before(*task.StartedAt)) {
			return ErrTime
		}
		summary, _ := json.Marshal(map[string]string{"failure": reason})
		return exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET status='failed',finished_at=$2,summary=$3::jsonb WHERE id=$1 AND status='running'", task.ID, finished, string(summary)))
	})
}

// RestartFailed creates a new accepted task linked to the immutable failed
// history and atomically releases the old fence. Call only after all old
// work/SQL has stopped. New acquisition never uses predecessor partial pages.
func (r Repository) RestartFailed(ctx context.Context, request Request) (Acceptance, error) {
	if r.owner == nil {
		return Acceptance{}, ErrOwnership
	}
	if strings.TrimSpace(request.ID) == "" || len(request.ID) > 64 || request.ID == r.owner.TaskID || strings.TrimSpace(request.IdempotencyKey) == "" || strings.TrimSpace(request.RequestedBy) == "" || request.AcceptedAt.IsZero() || request.PredecessorID == nil || *request.PredecessorID != r.owner.TaskID {
		return Acceptance{}, ErrEntity
	}
	var out Acceptance
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		current, err := readExecution(ctx, tx)
		if err != nil {
			return err
		}
		held := current == *r.owner
		if !held && (current.TaskID != "" || current.Generation != r.owner.Generation) {
			return ErrOwnership
		}
		old, err := loadTask(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		if old.Status != "failed" || request.Mode != old.Mode || request.AcceptedAt.Before(old.AcceptedAt) {
			return ErrEntity
		}
		var pending bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.tasks WHERE idempotency_key=$1)", request.IdempotencyKey).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if !held {
				return ErrOwnership
			}
			var unfinished bool
			if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.tasks WHERE status NOT IN ('failed','success_published','success_unchanged'))").Scan(&unfinished); err != nil {
				return err
			}
			if unfinished {
				return ErrBusy
			}
		}
		count, err := tx.Exec(ctx, "INSERT INTO control.tasks (id,idempotency_key,mode,status,requested_by,predecessor_id,accepted_at) VALUES ($1,$2,$3,'accepted',$4,$5,$6) ON CONFLICT (idempotency_key) DO NOTHING", request.ID, request.IdempotencyKey, request.Mode, request.RequestedBy, *request.PredecessorID, request.AcceptedAt)
		if err != nil {
			return err
		}
		d, _ := describe(&Task{})
		v, err := readEntity(ctx, tx, d, `"idempotency_key"=$1`, []any{request.IdempotencyKey}, false)
		if err != nil {
			return err
		}
		task := v.Interface().(Task)
		if task.Mode != request.Mode || task.RequestedBy != request.RequestedBy || !sameString(task.PredecessorID, request.PredecessorID) {
			return ErrIdempotency
		}
		if count < 0 || count > 1 {
			return ErrEntity
		}
		out = Acceptance{Task: task, Created: count == 1}
		if held {
			if err = exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET owner_id=NULL WHERE id=$1 AND owner_id=$2 AND owner_generation=$3", r.owner.TaskID, r.owner.OwnerID, r.owner.Generation)); err != nil {
				return err
			}
			if err = exactlyOne(tx.Exec(ctx, "UPDATE control.execution SET task_id=NULL,owner_id=NULL WHERE singleton=TRUE AND task_id=$1 AND owner_id=$2 AND generation=$3", r.owner.TaskID, r.owner.OwnerID, r.owner.Generation)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Acceptance{}, err
	}
	return out, nil
}
