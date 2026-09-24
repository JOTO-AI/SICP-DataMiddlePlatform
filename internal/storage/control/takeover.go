package control

import (
	"context"
	"math"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// Takeover may be called only after the old process AND SQL have been proven
// stopped. Expected is an exact snapshot fence, never an expiry or heartbeat.
// A duplicate with the same new worker is idempotent; stale workers cannot win.
func (r Repository) Takeover(ctx context.Context, expected Ownership, worker string) (Ownership, error) {
	if validateOwnership(expected) != nil || strings.TrimSpace(worker) == "" || worker == expected.OwnerID || expected.Generation == math.MaxInt64 {
		return Ownership{}, ErrOwnership
	}
	next := Ownership{TaskID: expected.TaskID, OwnerID: worker, Generation: expected.Generation + 1}
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		current, err := readExecution(ctx, tx)
		if err != nil {
			return err
		}
		if current == next {
			return nil
		}
		if current != expected {
			return ErrOwnership
		}
		task, err := loadTask(ctx, tx, expected.TaskID)
		if err != nil {
			return err
		}
		if task.OwnerID == nil || *task.OwnerID != expected.OwnerID || task.OwnerGeneration != expected.Generation {
			return ErrOwnership
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.execution SET owner_id=$1,generation=$2 WHERE singleton=TRUE AND task_id=$3 AND owner_id=$4 AND generation=$5", worker, next.Generation, expected.TaskID, expected.OwnerID, expected.Generation)); err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET owner_id=$1,owner_generation=$2 WHERE id=$3 AND owner_generation=$4", worker, next.Generation, expected.TaskID, expected.Generation)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE control.candidates SET owner_generation=$1 WHERE task_id=$2 AND owner_generation=$3", next.Generation, expected.TaskID, expected.Generation)
		return err
	})
	if err != nil {
		return Ownership{}, err
	}
	return next, nil
}
