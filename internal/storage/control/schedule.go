package control

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

var ErrStaleTimer = errors.New("schedule timer revision is no longer current")

type Launch struct {
	Task              Task
	Owner             Ownership
	Started, Deferred bool
}

func loadSchedule(ctx context.Context, tx storage.Transaction, locked bool) (Schedule, error) {
	s := Schedule{Singleton: true}
	sql := "SELECT revision,last_started_at,next_due_at,deferred,cooldown_until FROM control.schedule WHERE singleton=TRUE"
	if locked {
		sql += " FOR UPDATE"
	}
	err := tx.QueryRow(ctx, sql).Scan(&s.Revision, &s.LastStartedAt, &s.NextDueAt, &s.Deferred, &s.CooldownUntil)
	return s, err
}
func (r Repository) CurrentSchedule(ctx context.Context) (Schedule, error) {
	var s Schedule
	err := r.transaction(ctx, func(tx storage.Transaction) error { var err error; s, err = loadSchedule(ctx, tx, false); return err })
	if err != nil {
		return Schedule{}, err
	}
	return s, nil
}

// Dispatch atomically admits and actually starts one task. Manual calls pass
// nil revision; timed calls pass the revision captured by their timer. Busy
// calls never enqueue tasks or move started_at; one deferred bit coalesces them.
func (r Repository) Dispatch(ctx context.Context, request Request, worker string, at time.Time, revision *int64) (Launch, error) {
	if strings.TrimSpace(request.ID) == "" || len(request.ID) > 64 || strings.TrimSpace(request.IdempotencyKey) == "" || strings.TrimSpace(request.RequestedBy) == "" || strings.TrimSpace(worker) == "" || request.AcceptedAt.IsZero() || at.IsZero() || (request.Mode != "initialize" && request.Mode != "incremental" && request.Mode != "force_full") {
		return Launch{}, ErrEntity
	}
	if at.Before(request.AcceptedAt) {
		return Launch{}, ErrTime
	}
	var out Launch
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
		s, err := loadSchedule(ctx, tx, true)
		if err != nil {
			return err
		}
		if revision != nil && (*revision != s.Revision || s.NextDueAt == nil || at.Before(*s.NextDueAt)) {
			return ErrStaleTimer
		}
		if s.LastStartedAt != nil && at.Before(*s.LastStartedAt) {
			return ErrTime
		}
		var pending, unfinished bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.tasks WHERE status IN ('accepted','running','published_pending_progress','recovery_pending'))").Scan(&unfinished); err != nil {
			return err
		}
		if owner.TaskID != "" || pending || unfinished || s.CooldownUntil != nil && at.Before(*s.CooldownUntil) {
			if s.NextDueAt != nil && !at.Before(*s.NextDueAt) && !s.Deferred {
				if err = exactlyOne(tx.Exec(ctx, "UPDATE control.schedule SET deferred=TRUE WHERE singleton=TRUE AND deferred=FALSE")); err != nil {
					return err
				}
			}
			out.Deferred = true
			return nil
		}
		if owner.Generation == math.MaxInt64 || s.Revision == math.MaxInt64 {
			return ErrOwnership
		}
		if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.tasks (id,idempotency_key,mode,status,requested_by,predecessor_id,accepted_at) VALUES ($1,$2,$3,'accepted',$4,$5,$6) ON CONFLICT (idempotency_key) DO NOTHING", request.ID, request.IdempotencyKey, request.Mode, request.RequestedBy, optionalString(request.PredecessorID), request.AcceptedAt)); err != nil {
			return err
		}
		next := Ownership{TaskID: request.ID, OwnerID: worker, Generation: owner.Generation + 1}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.execution SET task_id=$1,owner_id=$2,generation=$3 WHERE singleton=TRUE AND generation=$4 AND task_id IS NULL AND owner_id IS NULL", request.ID, worker, next.Generation, owner.Generation)); err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET owner_id=$1,owner_generation=$2 WHERE id=$3 AND owner_generation=$4", worker, next.Generation, request.ID, int64(0))); err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET started_at=$1,status='running' WHERE id=$2 AND owner_id=$3 AND owner_generation=$4 AND started_at IS NULL", at, request.ID, worker, next.Generation)); err != nil {
			return err
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.schedule SET revision=revision+1,last_started_at=$1,next_due_at=$2,deferred=FALSE WHERE singleton=TRUE AND (last_started_at IS NULL OR last_started_at <= $1)", at, at.Add(SyncInterval))); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, request.ID)
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
