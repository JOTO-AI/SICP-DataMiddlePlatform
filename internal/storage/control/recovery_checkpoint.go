package control

import (
	"context"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// RecoveryCheckpoint stores orchestration evidence separately from task roles.
// All access is fenced; payloads cannot grant physical overwrite permission.
func (r Repository) RecoveryCheckpoint(ctx context.Context, save *Document, at time.Time) (Document, error) {
	if r.owner == nil {
		return "", ErrOwnership
	}
	if save != nil && (len(*save) > 1<<20 || !json.Valid([]byte(*save)) || at.IsZero()) {
		return "", ErrEntity
	}
	var out string
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		if save != nil {
			task, err := loadTask(ctx, tx, r.owner.TaskID)
			if err != nil {
				return err
			}
			if task.Status != "running" && task.Status != "published_pending_progress" {
				return ErrEntity
			}
			if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.checkpoints (task_id,name,payload,recorded_at) VALUES ($1,'update-local',$2::jsonb,$3) ON CONFLICT (task_id,name) DO UPDATE SET payload=EXCLUDED.payload,recorded_at=EXCLUDED.recorded_at", r.owner.TaskID, string(*save), at)); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, "SELECT payload::text FROM control.checkpoints WHERE task_id=$1 AND name='update-local'", r.owner.TaskID).Scan(&out)
	})
	if err != nil {
		return "", err
	}
	return Document(out), nil
}

func (r Repository) RecoveryPublication(ctx context.Context, id string) (*Publication, error) {
	if r.owner == nil {
		return nil, ErrOwnership
	}
	var out *Publication
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE id=$1)", id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return nil
		}
		d, _ := describe(&Publication{})
		v, err := readEntity(ctx, tx, d, `"id"=$1`, []any{id}, false)
		if err != nil {
			return err
		}
		event := v.Interface().(Publication)
		if event.TaskID != r.owner.TaskID {
			return ErrOwnership
		}
		out = &event
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
