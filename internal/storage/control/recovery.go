package control

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// RecoveryState is a consistent control snapshot, not permission to mutate a
// slot. Every later mutation must reacquire its ownership and physical gates.
type RecoveryState struct {
	Owner     Ownership
	Task      *Task
	Pending   *Publication
	Confirmed *ActiveMetadata
}

func (r Repository) RecoveryState(ctx context.Context) (RecoveryState, error) {
	var out RecoveryState
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		var err error
		out.Owner, err = readExecution(ctx, tx)
		if err != nil {
			return err
		}
		var id *string
		var count int64
		if err = tx.QueryRow(ctx, "SELECT min(id),count(*) FROM control.tasks WHERE status NOT IN ('failed','success_published','success_unchanged')").Scan(&id, &count); err != nil {
			return err
		}
		if count > 1 {
			return ErrBusy
		}
		if out.Owner.TaskID != "" {
			if id != nil && *id != out.Owner.TaskID {
				return ErrOwnership
			}
			id = &out.Owner.TaskID
		}
		if id != nil {
			task, err := loadTask(ctx, tx, *id)
			if err != nil {
				return err
			}
			out.Task = &task
			if out.Owner.TaskID != "" && (task.OwnerID == nil || *task.OwnerID != out.Owner.OwnerID || task.OwnerGeneration != out.Owner.Generation) {
				return ErrOwnership
			}
			if out.Owner.TaskID == "" && task.OwnerID != nil {
				return ErrOwnership
			}
		}
		var pending *string
		if err = tx.QueryRow(ctx, "SELECT (SELECT id FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if pending != nil {
			d, _ := describe(&Publication{})
			v, err := readEntity(ctx, tx, d, `"id"=$1`, []any{*pending}, true)
			if err != nil {
				return err
			}
			event := v.Interface().(Publication)
			if out.Task == nil || event.TaskID != out.Task.ID || event.Status != "pending" {
				return ErrEntity
			}
			out.Pending = &event
		}
		var raw *string
		if err = tx.QueryRow(ctx, "SELECT (SELECT target::text FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence DESC LIMIT 1)").Scan(&raw); err != nil {
			return err
		}
		if raw != nil {
			active, err := DecodeActive(Document(*raw))
			if err != nil {
				return err
			}
			out.Confirmed = &active
		}
		return nil
	})
	if err != nil {
		return RecoveryState{}, err
	}
	return out, nil
}
