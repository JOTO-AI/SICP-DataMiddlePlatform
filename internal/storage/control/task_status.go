package control

import (
	"context"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type TaskStatus struct {
	Task     Task
	Progress *ProgressPlan
}

func existingTask(ctx context.Context, tx storage.Transaction, id string) (Task, error) {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.tasks WHERE id=$1)", id).Scan(&exists); err != nil {
		return Task{}, err
	}
	if !exists {
		return Task{}, ErrMissing
	}
	return loadTask(ctx, tx, id)
}
func (r Repository) TaskStatus(ctx context.Context, id string) (TaskStatus, error) {
	if strings.TrimSpace(id) == "" || len(id) > 64 {
		return TaskStatus{}, ErrEntity
	}
	var out TaskStatus
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		task, err := existingTask(ctx, tx, id)
		if err != nil {
			return err
		}
		out.Task = task
		// Historical task progress comes from its own immutable confirmed plan,
		// never the source_progress rows that a newer task may have advanced.
		if task.Status == "success_published" || task.Status == "success_unchanged" {
			plan, err := loadProgressPlan(ctx, tx, id)
			if err != nil {
				return err
			}
			if task.StartedAt == nil || !plan.StartedAt.Equal(*task.StartedAt) || plan.Mode != task.Mode || task.CheckedAt == nil || !plan.CheckedAt.Equal(*task.CheckedAt) {
				return ErrEntity
			}
			out.Progress = &plan
		}
		return nil
	})
	if err != nil {
		return TaskStatus{}, err
	}
	return out, nil
}
