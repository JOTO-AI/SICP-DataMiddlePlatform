package control

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

var ErrInitializeRequired = errors.New("initialize complete data before requesting an update")
var ErrAlreadyInitialized = errors.New("complete data is already initialized")

// Admit persists acceptance only. Client cancellation after commit cannot
// cancel the separately owned worker task.
func (r Repository) Admit(ctx context.Context, request Request) (Acceptance, error) {
	if strings.TrimSpace(request.ID) == "" || len(request.ID) > 64 || strings.TrimSpace(request.IdempotencyKey) == "" || len(request.IdempotencyKey) > 128 || strings.TrimSpace(request.RequestedBy) == "" || request.AcceptedAt.IsZero() || request.PredecessorID != nil || (request.Mode != "initialize" && request.Mode != "incremental" && request.Mode != "force_full") {
		return Acceptance{}, ErrEntity
	}
	var out Acceptance
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		owner, err := readExecution(ctx, tx)
		if err != nil {
			return err
		}
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.tasks WHERE idempotency_key=$1)", request.IdempotencyKey).Scan(&exists); err != nil {
			return err
		}
		if exists {
			d, _ := describe(&Task{})
			v, err := readEntity(ctx, tx, d, `"idempotency_key"=$1`, []any{request.IdempotencyKey}, false)
			if err != nil {
				return err
			}
			task := v.Interface().(Task)
			if task.Mode != request.Mode || task.RequestedBy != request.RequestedBy || !sameString(task.PredecessorID, request.PredecessorID) {
				return ErrIdempotency
			}
			out.Task = task
			return nil
		}
		var pending, unfinished, initialized bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.tasks WHERE status NOT IN ('failed','success_published','success_unchanged'))").Scan(&unfinished); err != nil {
			return err
		}
		if owner.TaskID != "" || pending || unfinished {
			return ErrBusy
		}
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='confirmed' AND kind='bundle_publish')").Scan(&initialized); err != nil {
			return err
		}
		if !initialized && request.Mode != "initialize" {
			return ErrInitializeRequired
		}
		if initialized && request.Mode == "initialize" {
			return ErrAlreadyInitialized
		}
		if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.tasks (id,idempotency_key,mode,status,requested_by,predecessor_id,accepted_at) VALUES ($1,$2,$3,'accepted',$4,$5,$6) ON CONFLICT (idempotency_key) DO NOTHING", request.ID, request.IdempotencyKey, request.Mode, request.RequestedBy, nil, request.AcceptedAt)); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, request.ID)
		if err != nil {
			return err
		}
		out = Acceptance{Task: task, Created: true}
		return nil
	})
	if err != nil {
		return Acceptance{}, err
	}
	return out, nil
}

// StartAccepted uses the worker lifecycle context, never the HTTP context.
func (r Repository) StartAccepted(ctx context.Context, id, worker string, at time.Time) (Launch, error) {
	if id == "" || strings.TrimSpace(worker) == "" || at.IsZero() {
		return Launch{}, ErrEntity
	}
	var out Launch
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		owner, err := readExecution(ctx, tx)
		if err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, id)
		if err != nil {
			return err
		}
		if owner.TaskID != "" {
			if owner.TaskID == id && owner.OwnerID == worker && task.Status == "running" && task.StartedAt != nil && task.OwnerID != nil && *task.OwnerID == owner.OwnerID && task.OwnerGeneration == owner.Generation {
				out = Launch{Task: task, Owner: owner}
				return nil
			}
			return ErrBusy
		}
		if task.Status != "accepted" || task.StartedAt != nil || task.OwnerID != nil || task.OwnerGeneration > owner.Generation {
			return ErrEntity
		}
		var pending bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		s, err := loadSchedule(ctx, tx, true)
		if err != nil {
			return err
		}
		if s.CooldownUntil != nil && at.Before(*s.CooldownUntil) {
			return ErrBusy
		}
		if at.Before(task.AcceptedAt) || s.LastStartedAt != nil && at.Before(*s.LastStartedAt) {
			return ErrTime
		}
		if owner.Generation == math.MaxInt64 || s.Revision == math.MaxInt64 {
			return ErrOwnership
		}
		next := Ownership{TaskID: id, OwnerID: worker, Generation: owner.Generation + 1}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.execution SET task_id=$1,owner_id=$2,generation=$3 WHERE singleton=TRUE AND generation=$4 AND task_id IS NULL AND owner_id IS NULL", id, worker, next.Generation, owner.Generation)); err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET owner_id=$1,owner_generation=$2 WHERE id=$3 AND owner_generation=$4", worker, next.Generation, id, task.OwnerGeneration)); err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET started_at=$1,status='running' WHERE id=$2 AND owner_id=$3 AND owner_generation=$4 AND started_at IS NULL", at, id, worker, next.Generation)); err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.schedule SET revision=revision+1,last_started_at=$1,next_due_at=$2,deferred=FALSE WHERE singleton=TRUE AND (last_started_at IS NULL OR last_started_at <= $1)", at, at.Add(SyncInterval))); err != nil {
			return err
		}
		task, err = loadTask(ctx, tx, id)
		if err != nil {
			return err
		}
		out = Launch{Task: task, Owner: next, Started: true}
		return nil
	})
	if err != nil {
		return Launch{}, err
	}
	return out, nil
}
