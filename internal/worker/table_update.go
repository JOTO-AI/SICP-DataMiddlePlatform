package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

func (w Executor) configureTables(ctx context.Context, task control.Task, repo control.Repository, files *staging.Store, a *update.Acquirer) (time.Time, error) {
	selected, err := repo.TableSelection(ctx, task.ID)
	if err != nil {
		return time.Time{}, err
	}
	a.OnlyTable = selected
	if task.Mode == "initialize" || task.Mode == "force_full" {
		a.ContinueOnFailure = true
		a.NewBudget = func() (*ingest.TaskBudget, error) { return ingest.NewTaskBudget(w.Clock.Now().Add(time.Hour), 100000) }
	}
	if task.Mode != "initialize" {
		return time.Time{}, nil
	}
	ancestors, epoch, err := initializationAncestors(ctx, task, repo, w.Runtime.Files.Staging)
	if err != nil {
		return time.Time{}, err
	}
	a.Reuse = func(ctx context.Context, r ingest.PageRequest, observe staging.TableCopyObserver) (int64, error) {
		return ancestors.copyTable(ctx, r, files, a.Limits, observe)
	}
	if err := repo.SaveSourceEpoch(ctx, epoch); err != nil {
		return time.Time{}, err
	}
	return epoch, nil
}

func fullUpdateAncestors(ctx context.Context, task control.Task, plan control.UpdatePlan, repo tableHistory, files *staging.QuotaFiles) (initializationHistory, bool, error) {
	var history initializationHistory
	baseline := false
	seen := map[string]bool{task.ID: true}
	for id := task.PredecessorID; id != nil; {
		if seen[*id] {
			return nil, false, control.ErrEntity
		}
		seen[*id] = true
		old, err := repo.TaskStatus(ctx, *id)
		if err != nil {
			return nil, false, err
		}
		if old.Task.ID != *id || old.Task.Mode != "force_full" || old.Task.Status != "failed" {
			return nil, false, control.ErrEntity
		}
		if old.Task.Roles != nil {
			var prior control.UpdatePlan
			if json.Unmarshal([]byte(*old.Task.Roles), &prior) != nil || prior.Validate() != nil {
				return nil, false, control.ErrEntity
			}
			if !plan.SameRepairBase(prior) {
				break
			}
			baseline = prior.Table != "" && !prior.Repair
		}
		selected, err := repo.TableSelection(ctx, *id)
		if err != nil {
			return nil, false, err
		}
		from, err := staging.NewStore(*id, files, 64<<20)
		if err != nil {
			return nil, false, err
		}
		history = append(history, initializationAncestor{from, selected})
		id = old.Task.PredecessorID
	}
	return history, baseline, nil
}

func (h initializationHistory) selected(table string) bool {
	for _, old := range h {
		if old.selected == table {
			return true
		}
	}
	return false
}

type tableHistory interface {
	TaskStatus(context.Context, string) (control.TaskStatus, error)
	TableSelection(context.Context, string) (string, error)
	SourceEpoch(context.Context, string) (*time.Time, error)
}
type initializationAncestor struct {
	store    *staging.Store
	selected string
}
type initializationHistory []initializationAncestor

// Follow the durable lineage, including tasks interrupted before they copied
// every completed table. Their start times alone are not safe source watermarks.
func initializationAncestors(ctx context.Context, task control.Task, repo tableHistory, files *staging.QuotaFiles) (initializationHistory, time.Time, error) {
	if task.StartedAt == nil {
		return nil, time.Time{}, control.ErrEntity
	}
	epoch := *task.StartedAt
	var ancestors initializationHistory
	seen := map[string]bool{task.ID: true}
	for id := task.PredecessorID; id != nil; {
		if seen[*id] {
			return nil, time.Time{}, control.ErrEntity
		}
		seen[*id] = true
		old, err := repo.TaskStatus(ctx, *id)
		if err != nil {
			return nil, time.Time{}, err
		}
		if old.Task.ID != *id || old.Task.Mode != "initialize" || old.Task.Status != "failed" {
			return nil, time.Time{}, control.ErrEntity
		}
		if old.Task.StartedAt != nil && old.Task.StartedAt.Before(epoch) {
			epoch = *old.Task.StartedAt
		}
		prior, err := repo.SourceEpoch(ctx, *id)
		if err != nil {
			return nil, time.Time{}, err
		}
		if prior != nil && prior.Before(epoch) {
			epoch = *prior
		}
		selected, err := repo.TableSelection(ctx, *id)
		if err != nil {
			return nil, time.Time{}, err
		}
		from, err := staging.NewStore(*id, files, 64<<20)
		if err != nil {
			return nil, time.Time{}, err
		}
		ancestors = append(ancestors, initializationAncestor{from, selected})
		id = old.Task.PredecessorID
	}
	return ancestors, epoch, nil
}

func (h initializationHistory) copyTable(ctx context.Context, r ingest.PageRequest, target *staging.Store, limits ingest.PageLimits, observe staging.TableCopyObserver) (int64, error) {
	for _, old := range h {
		pages, err := old.store.CopyTableWithProgress(ctx, r.Table, target, r.Mode, limits, observe)
		if !errors.Is(err, staging.ErrIncomplete) {
			return pages, err
		}
		// An explicit refresh invalidates its older version, even if interrupted.
		if old.selected == r.Table {
			return 0, staging.ErrIncomplete
		}
	}
	return 0, staging.ErrIncomplete
}
