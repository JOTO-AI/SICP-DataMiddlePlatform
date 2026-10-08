package control

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

var ErrBusy = errors.New("another executor holds control ownership")

// WithOwnership returns an independent repository view carrying an immutable
// fencing token. Candidate and progress mutations require this view.
func (r Repository) WithOwnership(owner Ownership) Repository { r.owner = &owner; return r }

func (r Repository) validateWriter(entity any) error {
	if _, err := describe(entity); err != nil {
		return err
	}
	var taskID string
	required := false
	switch v := entity.(type) {
	case *Execution:
		return ErrOwnership // Only Acquire/Release may mutate the execution fence.
	case *Candidate:
		taskID = v.TaskID
		required = true
	case *Progress:
		taskID = v.TaskID
		required = true
	case *Task:
		taskID = v.ID
	case *Step:
		taskID = v.TaskID
	case *Checkpoint:
		taskID = v.TaskID
	case *Publication:
		taskID = v.TaskID
	case *Notification:
		taskID = v.TaskID
	}
	if r.owner == nil {
		if required {
			return ErrOwnership
		}
		return nil
	}
	if err := validateOwnership(*r.owner); err != nil {
		return err
	}
	if (required || taskID != "") && taskID != r.owner.TaskID {
		return ErrOwnership
	}
	if v, ok := entity.(*Candidate); ok && v.OwnerGeneration != r.owner.Generation {
		return ErrOwnership
	}
	return nil
}
func (r Repository) fenceWriter(ctx context.Context, tx storage.Transaction) error {
	if r.owner == nil {
		return nil
	}
	return lockOwnership(ctx, tx, *r.owner)
}

func readExecution(ctx context.Context, tx storage.Transaction) (Ownership, error) {
	defer buildmetric.LockAcquisition(ctx)()
	var taskID, ownerID *string
	var generation int64
	if err := tx.QueryRow(ctx, "SELECT task_id,owner_id,generation FROM control.execution WHERE singleton=TRUE FOR UPDATE").Scan(&taskID, &ownerID, &generation); err != nil {
		return Ownership{}, err
	}
	if (taskID == nil) != (ownerID == nil) || generation < 0 {
		return Ownership{}, ErrOwnership
	}
	result := Ownership{Generation: generation}
	if taskID != nil {
		if strings.TrimSpace(*taskID) == "" || strings.TrimSpace(*ownerID) == "" || generation < 1 {
			return Ownership{}, ErrOwnership
		}
		result.TaskID = *taskID
		result.OwnerID = *ownerID
	}
	return result, nil
}

// Acquire never steals an occupied singleton, even after a process disappears.
// Recovery must establish execution termination before releasing its old owner.
func (r Repository) Acquire(ctx context.Context, taskID, ownerID string) (Ownership, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(ownerID) == "" {
		return Ownership{}, ErrOwnership
	}
	var granted Ownership
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		current, err := readExecution(ctx, tx)
		if err != nil {
			return err
		}
		if current.TaskID != "" {
			if current.TaskID == taskID && current.OwnerID == ownerID {
				granted = current
				return nil
			}
			return ErrBusy
		}
		if current.Generation == math.MaxInt64 {
			return ErrOwnership
		}
		d, _ := describe(&Task{})
		loaded, err := readEntity(ctx, tx, d, `"id"=$1`, []any{taskID}, true)
		if err != nil {
			return err
		}
		task := loaded.Interface().(Task)
		if task.OwnerID != nil || task.OwnerGeneration > current.Generation {
			return ErrOwnership
		}
		if (task.Status != "accepted" && task.Status != "running") || task.FinishedAt != nil {
			return ErrEntity
		}
		granted = Ownership{TaskID: taskID, OwnerID: ownerID, Generation: current.Generation + 1}
		count, err := tx.Exec(ctx, "UPDATE control.execution SET task_id=$1,owner_id=$2,generation=$3 WHERE singleton=TRUE AND generation=$4 AND task_id IS NULL AND owner_id IS NULL", taskID, ownerID, granted.Generation, current.Generation)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrBusy
		}
		count, err = tx.Exec(ctx, "UPDATE control.tasks SET owner_id=$1,owner_generation=$2 WHERE id=$3 AND owner_generation=$4", ownerID, granted.Generation, taskID, task.OwnerGeneration)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrOwnership
		}
		return nil
	})
	if err != nil {
		return Ownership{}, err
	}
	return granted, nil
}

// Release is called only after the owner's work/SQL has actually terminated.
// It leaves generation and historical times intact. Releasing the same completed
// ownership twice is harmless; a late release cannot clear a newer owner.
func (r Repository) Release(ctx context.Context, owner Ownership) error {
	if err := validateOwnership(owner); err != nil {
		return err
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		current, err := readExecution(ctx, tx)
		if err != nil {
			return err
		}
		if current.TaskID == "" && current.Generation == owner.Generation {
			return nil
		}
		if current != owner {
			return ErrOwnership
		}
		count, err := tx.Exec(ctx, "UPDATE control.tasks SET owner_id=NULL WHERE id=$1 AND owner_id=$2 AND owner_generation=$3", owner.TaskID, owner.OwnerID, owner.Generation)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrOwnership
		}
		count, err = tx.Exec(ctx, "UPDATE control.execution SET task_id=NULL,owner_id=NULL WHERE singleton=TRUE AND task_id=$1 AND owner_id=$2 AND generation=$3", owner.TaskID, owner.OwnerID, owner.Generation)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrOwnership
		}
		return nil
	})
}
