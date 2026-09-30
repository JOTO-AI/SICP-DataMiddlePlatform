package control

import (
	"context"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type SourceWindow struct {
	ConfirmedUntil time.Time
	Start, End     time.Time
}

// UpdatePlan is saved once before acquisition. Roles describe the task's
// original direction even after its route_shift becomes the current route.
type UpdatePlan struct {
	TaskID        string
	Mode          string
	StartedAt     time.Time
	Origin        ActiveMetadata
	First, Second storage.Instance
	Overlap       time.Duration
	Windows       map[string]SourceWindow
	Table         string
	Repair        bool
}

func (p UpdatePlan) Validate() error {
	if p.TaskID == "" || p.StartedAt.IsZero() || p.Origin.Validate() != nil || p.Overlap <= 0 || p.Overlap > 24*time.Hour || len(p.Windows) != len(source.Catalog()) {
		return ErrEntity
	}
	if p.Mode != "incremental" && p.Mode != "force_full" {
		return ErrEntity
	}
	if p.Table != "" && (p.Mode != "force_full" || !knownTable(p.Table)) {
		return ErrEntity
	}
	if p.Repair && p.Mode != "force_full" {
		return ErrEntity
	}
	if p.Second != p.Origin.Position.Position.Instance || p.First == p.Second || (p.First != storage.Main && p.First != storage.Standby) {
		return ErrEntity
	}
	for _, table := range source.Catalog() {
		w, ok := p.Windows[table.Name]
		if !ok || w.ConfirmedUntil.IsZero() || !w.End.Equal(p.StartedAt) || !w.Start.Equal(w.ConfirmedUntil.Add(-p.Overlap)) {
			return ErrEntity
		}
		// Compare with successful progress, not the overlapped start: an
		// apparently nonempty overlap must not hide a clock rollback.
		if p.StartedAt.Before(w.ConfirmedUntil) {
			return ErrTime
		}
	}
	return nil
}

// PlanUpdate reads successful progress and saves immutable roles/windows in a
// fenced control transaction. A failed commit returns no usable plan. Recovery
// loads the original plan rather than inferring roles from the new live route.
// The caller supplies acknowledged active metadata; pending publications block
// first planning. An existing plan is recovery input, not permission to execute.
func (r Repository) PlanUpdate(ctx context.Context, active ActiveMetadata, overlap time.Duration) (UpdatePlan, error) {
	if r.owner == nil {
		return UpdatePlan{}, ErrOwnership
	}
	if active.Validate() != nil || overlap < 0 || overlap > 24*time.Hour {
		return UpdatePlan{}, ErrEntity
	}
	if overlap == 0 {
		overlap = IncrementalOverlap
	}
	var plan UpdatePlan
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		if task.StartedAt == nil || task.Status != "running" || (task.Mode != "incremental" && task.Mode != "force_full") {
			return ErrEntity
		}
		if task.Roles != nil {
			if json.Unmarshal([]byte(*task.Roles), &plan) != nil || plan.Validate() != nil || plan.TaskID != task.ID || plan.Mode != task.Mode || !plan.StartedAt.Equal(*task.StartedAt) {
				return ErrEntity
			}
			return nil
		}
		var pending bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		var confirmed *string
		if err = tx.QueryRow(ctx, "SELECT (SELECT target::text FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence DESC LIMIT 1)").Scan(&confirmed); err != nil {
			return err
		}
		if confirmed == nil {
			return ErrEntity
		}
		current, err := DecodeActive(Document(*confirmed))
		if err != nil || current != active {
			return ErrIdempotency
		}
		first := storage.Standby
		if active.Position.Position.Instance == storage.Standby {
			first = storage.Main
		}
		plan = UpdatePlan{TaskID: task.ID, Mode: task.Mode, StartedAt: *task.StartedAt, Origin: active, First: first, Second: active.Position.Position.Instance, Overlap: overlap, Windows: map[string]SourceWindow{}}
		plan.Table, err = tableSelection(ctx, tx, task.ID)
		if err != nil {
			return err
		}
		for _, table := range source.Catalog() {
			var until time.Time
			if err = tx.QueryRow(ctx, "SELECT confirmed_until FROM control.source_progress WHERE source=$1 FOR SHARE", table.Name).Scan(&until); err != nil {
				return err
			}
			plan.Windows[table.Name] = SourceWindow{ConfirmedUntil: until, Start: until.Add(-overlap), End: plan.StartedAt}
		}
		if task.Mode == "force_full" {
			seen := map[string]bool{task.ID: true}
			for id := task.PredecessorID; id != nil; {
				if seen[*id] {
					return ErrEntity
				}
				seen[*id] = true
				old, err := loadTask(ctx, tx, *id)
				if err != nil {
					return err
				}
				if old.Mode != "force_full" || old.Status != "failed" {
					return ErrEntity
				}
				if old.Roles != nil {
					var prior UpdatePlan
					if json.Unmarshal([]byte(*old.Roles), &prior) != nil || prior.Validate() != nil {
						return ErrEntity
					}
					plan.Repair = plan.SameRepairBase(prior)
					break
				}
				id = old.PredecessorID
			}
		}
		if err = plan.Validate(); err != nil {
			return err
		}
		raw, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		return exactlyOne(tx.Exec(ctx, "UPDATE control.tasks SET roles=$2::jsonb WHERE id=$1 AND roles IS NULL AND status='running'", task.ID, string(raw)))
	})
	if err != nil {
		return UpdatePlan{}, err
	}
	return plan, nil
}

// Reuse is safe only while neither the published content nor its successful
// source progress has changed since the failed full acquisition was planned.
func (p UpdatePlan) SameRepairBase(old UpdatePlan) bool {
	if p.Mode != "force_full" || old.Mode != "force_full" || p.Origin.Position.BundleID != old.Origin.Position.BundleID || len(p.Windows) != len(old.Windows) {
		return false
	}
	for name, window := range p.Windows {
		prior, ok := old.Windows[name]
		if !ok || !window.ConfirmedUntil.Equal(prior.ConfirmedUntil) {
			return false
		}
	}
	return true
}
