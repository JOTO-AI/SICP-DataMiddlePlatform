package control

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

var ErrIdempotency = errors.New("idempotency key belongs to a different request")
var ErrOwnership = errors.New("execution ownership is not current")
var ErrTime = errors.New("control time would move backwards")

const SyncInterval = 2 * time.Hour

type Request struct {
	ID, IdempotencyKey, Mode, RequestedBy string
	PredecessorID                         *string
	AcceptedAt                            time.Time
	Table                                 string
}
type Acceptance struct {
	Task    Task
	Created bool
}
type Ownership struct {
	TaskID, OwnerID string
	Generation      int64
}

func (r Repository) Accept(ctx context.Context, request Request) (Acceptance, error) {
	if request.Table != "" {
		return Acceptance{}, ErrEntity
	}
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.IdempotencyKey) == "" || strings.TrimSpace(request.RequestedBy) == "" || request.AcceptedAt.IsZero() {
		return Acceptance{}, ErrEntity
	}
	if request.Mode != "initialize" && request.Mode != "incremental" && request.Mode != "force_full" {
		return Acceptance{}, ErrEntity
	}
	var result Acceptance
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		// A unique idempotency key serializes identical concurrent submissions.
		// This statement never writes started_at or changes schedule state.
		count, err := tx.Exec(ctx, "INSERT INTO control.tasks (id,idempotency_key,mode,status,requested_by,predecessor_id,accepted_at) VALUES ($1,$2,$3,'accepted',$4,$5,$6) ON CONFLICT (idempotency_key) DO NOTHING", request.ID, request.IdempotencyKey, request.Mode, request.RequestedBy, optionalString(request.PredecessorID), request.AcceptedAt)
		if err != nil {
			return err
		}
		if count < 0 || count > 1 {
			return ErrEntity
		}
		d, _ := describe(&Task{})
		loaded, err := readEntity(ctx, tx, d, `"idempotency_key"=$1`, []any{request.IdempotencyKey}, false)
		if err != nil {
			return err
		}
		task := loaded.Interface().(Task)
		if task.Mode != request.Mode || task.RequestedBy != request.RequestedBy || !sameString(task.PredecessorID, request.PredecessorID) {
			return ErrIdempotency
		}
		result = Acceptance{Task: task, Created: count == 1}
		return nil
	})
	if err != nil {
		return Acceptance{}, err
	}
	return result, nil
}

func optionalString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}
func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func validateOwnership(owner Ownership) error {
	if strings.TrimSpace(owner.TaskID) == "" || strings.TrimSpace(owner.OwnerID) == "" || owner.Generation < 1 {
		return ErrOwnership
	}
	return nil
}

// lockOwnership keeps the fixed execution singleton locked until the caller's
// transaction ends. Ownership expiry alone never grants a new executor access.
func lockOwnership(ctx context.Context, tx storage.Transaction, owner Ownership) error {
	defer buildmetric.LockAcquisition(ctx)()
	if err := validateOwnership(owner); err != nil {
		return err
	}
	var taskID, ownerID *string
	var generation int64
	if err := tx.QueryRow(ctx, "SELECT task_id,owner_id,generation FROM control.execution WHERE singleton=TRUE FOR UPDATE").Scan(&taskID, &ownerID, &generation); err != nil {
		return err
	}
	if taskID == nil || ownerID == nil || *taskID != owner.TaskID || *ownerID != owner.OwnerID || generation != owner.Generation {
		return ErrOwnership
	}
	return nil
}

// Start is separate from acceptance. It requires execution ownership, persists
// the first actual start and updates the scheduler in the same transaction.
// Recovery with the same ownership returns the original time without rescheduling.
func (r Repository) Start(ctx context.Context, owner Ownership, at time.Time) (time.Time, error) {
	if err := validateOwnership(owner); err != nil {
		return time.Time{}, err
	}
	if at.IsZero() {
		return time.Time{}, ErrTime
	}
	var started time.Time
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := lockOwnership(ctx, tx, owner); err != nil {
			return err
		}
		d, _ := describe(&Task{})
		loaded, err := readEntity(ctx, tx, d, `"id"=$1`, []any{owner.TaskID}, true)
		if err != nil {
			return err
		}
		task := loaded.Interface().(Task)
		if task.OwnerID == nil || *task.OwnerID != owner.OwnerID || task.OwnerGeneration != owner.Generation {
			return ErrOwnership
		}
		if task.StartedAt != nil {
			started = *task.StartedAt
			return nil
		}
		if task.Status != "accepted" {
			return ErrEntity
		}
		if at.Before(task.AcceptedAt) {
			return ErrTime
		}
		count, err := tx.Exec(ctx, "UPDATE control.tasks SET started_at=$1,status='running' WHERE id=$2 AND owner_id=$3 AND owner_generation=$4 AND started_at IS NULL", at, owner.TaskID, owner.OwnerID, owner.Generation)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrOwnership
		}
		count, err = tx.Exec(ctx, "UPDATE control.schedule SET revision=revision+1,last_started_at=$1,next_due_at=$2,deferred=FALSE WHERE singleton=TRUE AND (last_started_at IS NULL OR last_started_at <= $1)", at, at.Add(SyncInterval))
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrTime
		}
		started = at
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	return started, nil
}
