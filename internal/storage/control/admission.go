package control

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

var ErrInitializeRequired = errors.New("initialize complete data before requesting an update")
var ErrAlreadyInitialized = errors.New("complete data is already initialized")
var ErrNotFailed = errors.New("only failed tasks can be rerun")

// Admit persists acceptance only. Client cancellation after commit cannot
// cancel the separately owned worker task.
func (r Repository) Admit(ctx context.Context, request Request) (Acceptance, error) {
	if request.PredecessorID != nil || request.Table != "" {
		return Acceptance{}, ErrEntity
	}
	return r.admit(ctx, request, false, false, false)
}

// Bootstrap admits the first task only in a pristine control database. The
// execution lock serializes this check with every admission and publication.
// Failed history is handled by RecoverInitialization, never a second bootstrap.
func (r Repository) Bootstrap(ctx context.Context, request Request) (Acceptance, error) {
	if request.Mode != "initialize" || request.RequestedBy != "scheduler" || request.PredecessorID != nil || request.Table != "" {
		return Acceptance{}, ErrEntity
	}
	return r.admit(ctx, request, false, true, false)
}

const InitializationRetryInterval = 5 * time.Minute

// RecoverInitialization is used once per worker lifetime after normal recovery
// has released the previous executor. It never treats existing history as empty.
func (r Repository) RecoverInitialization(ctx context.Context, request Request) (Acceptance, error) {
	if request.Mode != "initialize" || request.RequestedBy != "scheduler" || request.PredecessorID != nil || request.Table != "" {
		return Acceptance{}, ErrEntity
	}
	return r.admit(ctx, request, true, false, true)
}
func (r Repository) RerunTask(ctx context.Context, request Request) (Acceptance, error) {
	if request.PredecessorID == nil || strings.TrimSpace(*request.PredecessorID) == "" || len(*request.PredecessorID) > 64 || *request.PredecessorID == request.ID || request.Mode != "" || request.Table != "" {
		return Acceptance{}, ErrEntity
	}
	return r.admit(ctx, request, true, false, false)
}
func (r Repository) admit(ctx context.Context, request Request, rerun, bootstrap, automatic bool) (Acceptance, error) {
	if strings.TrimSpace(request.ID) == "" || len(request.ID) > 64 || strings.TrimSpace(request.IdempotencyKey) == "" || len(request.IdempotencyKey) > 128 || strings.TrimSpace(request.RequestedBy) == "" || request.AcceptedAt.IsZero() || (!rerun && request.Mode != "initialize" && request.Mode != "incremental" && request.Mode != "force_full") {
		return Acceptance{}, ErrEntity
	}
	var out Acceptance
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		owner, err := readExecution(ctx, tx)
		if err != nil {
			return err
		}
		if automatic {
			if owner.TaskID != "" || owner.OwnerID != "" {
				return nil
			}
			var latest *string
			if err = tx.QueryRow(ctx, "SELECT (SELECT id FROM control.tasks ORDER BY accepted_at DESC,id DESC LIMIT 1)").Scan(&latest); err != nil {
				return err
			}
			if latest == nil {
				return nil
			}
			if *latest == request.ID {
				return ErrEntity
			}
			old, err := loadTask(ctx, tx, *latest)
			if err != nil {
				return err
			}
			var summary struct {
				Failure string `json:"failure"`
			}
			if old.Mode != "initialize" || old.Status != "failed" || json.Unmarshal([]byte(old.Summary), &summary) != nil {
				return nil
			}
			switch summary.Failure {
			case "acquisition_failed", "corrupt_seal", "interrupted":
			default:
				return nil
			}
			last := old.AcceptedAt
			if old.StartedAt != nil {
				last = *old.StartedAt
			}
			if old.FinishedAt != nil {
				last = *old.FinishedAt
			}
			if request.AcceptedAt.Before(last.Add(InitializationRetryInterval)) {
				return nil
			}
			schedule, err := loadSchedule(ctx, tx, false)
			if err != nil {
				return err
			}
			if schedule.CooldownUntil != nil && request.AcceptedAt.Before(*schedule.CooldownUntil) {
				return nil
			}
			request.PredecessorID = latest
			request.IdempotencyKey = "system-initialize-retry-" + *latest
		}
		if bootstrap {
			var pristine bool
			if err = tx.QueryRow(ctx, `SELECT
 NOT EXISTS (SELECT 1 FROM control.tasks)
 AND NOT EXISTS (SELECT 1 FROM control.bundles)
 AND NOT EXISTS (SELECT 1 FROM control.publication_outbox)
 AND NOT EXISTS (SELECT 1 FROM control.source_progress)
 AND (SELECT count(*) FROM control.slots WHERE state='empty' AND generation=0 AND bundle_id IS NULL AND NOT admission_open)=4
 AND EXISTS (SELECT 1 FROM control.schedule WHERE singleton=TRUE AND revision=0 AND last_started_at IS NULL AND next_due_at IS NULL AND cooldown_until IS NULL AND NOT deferred)`).Scan(&pristine); err != nil {
				return err
			}
			if !pristine || owner.TaskID != "" || owner.Generation != 0 {
				return nil
			}
		}
		if rerun {
			old, err := existingTask(ctx, tx, *request.PredecessorID)
			if err != nil {
				return err
			}
			if old.Status != "failed" {
				return ErrNotFailed
			}
			if request.AcceptedAt.Before(old.AcceptedAt) {
				return ErrTime
			}
			request.Mode = old.Mode
			// Full/initialization reruns fill every remaining gap. The explicit
			// table endpoint is used when the operator wants only one table.
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
			selected, err := tableSelection(ctx, tx, task.ID)
			if err != nil {
				return err
			}
			if selected != request.Table || (request.Table == "" && task.Mode != request.Mode) || ((rerun || request.Table == "") && !sameString(task.PredecessorID, request.PredecessorID)) || task.RequestedBy != request.RequestedBy {
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
		if request.Table != "" && !rerun {
			if !knownTable(request.Table) {
				return ErrEntity
			}
			request.Mode = "force_full"
			if !initialized {
				request.Mode = "initialize"
			}
			var latest *string
			if err := tx.QueryRow(ctx, "SELECT (SELECT id FROM control.tasks ORDER BY accepted_at DESC,id DESC LIMIT 1)").Scan(&latest); err != nil {
				return err
			}
			if latest != nil {
				old, err := loadTask(ctx, tx, *latest)
				if err != nil {
					return err
				}
				if !initialized && (old.Mode != "initialize" || old.Status != "failed") {
					return ErrBusy
				}
				if old.Mode == request.Mode && old.Status == "failed" {
					request.PredecessorID = latest
				}
			}
		}
		if !initialized && request.Mode != "initialize" {
			return ErrInitializeRequired
		}
		if initialized && request.Mode == "initialize" {
			return ErrAlreadyInitialized
		}
		if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.tasks (id,idempotency_key,mode,status,requested_by,predecessor_id,accepted_at) VALUES ($1,$2,$3,'accepted',$4,$5,$6) ON CONFLICT (idempotency_key) DO NOTHING", request.ID, request.IdempotencyKey, request.Mode, request.RequestedBy, optionalString(request.PredecessorID), request.AcceptedAt)); err != nil {
			return err
		}
		if request.Table != "" {
			raw, _ := json.Marshal(struct {
				Table string `json:"table"`
			}{request.Table})
			if err = exactlyOne(tx.Exec(ctx, "INSERT INTO control.checkpoints (task_id,name,payload,recorded_at) VALUES ($1,'table-selection',$2::jsonb,$3)", request.ID, string(raw), request.AcceptedAt)); err != nil {
				return err
			}
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
