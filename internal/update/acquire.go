package update

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

// Acquire uses a fixed plan for all pages/retries, including distinct starts
// for individual sources. Source timestamps have explicit configurable zone
// and microsecond precision; full requests contain no time parameters.
func (a Acquirer) Acquire(ctx context.Context, plan control.UpdatePlan, zone *time.Location) (*staging.Lease, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if zone == nil {
		return nil, ErrInitialization
	}
	return a.collect(ctx, func(table string) ingest.PageRequest {
		r := ingest.PageRequest{Table: table, Mode: ingest.Mode(plan.Mode), Page: 1, PageSize: a.PageSize}
		if r.Mode == ingest.Incremental {
			w := plan.Windows[table]
			r.StartTime = w.Start.In(zone).Format("2006-01-02 15:04:05.999999")
			r.EndTime = w.End.In(zone).Format("2006-01-02 15:04:05.999999")
		}
		return r
	})
}

type UpdatePlanner interface {
	PlanUpdate(context.Context, control.ActiveMetadata, time.Duration) (control.UpdatePlan, error)
}
type PlannedSource interface {
	Acquire(context.Context, control.UpdatePlan, *time.Location) (*staging.Lease, error)
}
type Fetcher struct {
	Planner  UpdatePlanner
	Source   PlannedSource
	Preparer TargetPreparer
	Options  candidate.PrepareOptions
	Overlap  time.Duration
	Zone     *time.Location
}
type PreparedUpdate struct {
	Plan     control.UpdatePlan
	Manifest candidate.Manifest
	Lease    *staging.Lease
	Changes  map[string]candidate.ChangeCounts
}

type ChangeCounter interface {
	Changes(context.Context, candidate.Manifest) (map[string]candidate.ChangeCounts, error)
}

// Prepare is the acquisition barrier: comparison/merge sees only a verified
// fourteen-source seal. Failure exposes no partial target to later builders.
// Failed pages remain isolated by run ID for diagnosis, never resumable input.
func (f Fetcher) Prepare(ctx context.Context, active control.ActiveMetadata) (PreparedUpdate, error) {
	if f.Planner == nil || f.Source == nil || f.Preparer == nil || f.Zone == nil {
		return PreparedUpdate{}, ErrInitialization
	}
	p, err := f.Planner.PlanUpdate(ctx, active, f.Overlap)
	if err != nil {
		return PreparedUpdate{}, err
	}
	if err = p.Validate(); err != nil {
		return PreparedUpdate{}, err
	}
	l, err := f.Source.Acquire(ctx, p, f.Zone)
	if err != nil {
		return PreparedUpdate{}, err
	}
	if l == nil {
		return PreparedUpdate{}, ErrInitialization
	}
	if err = l.Validate(ctx); err != nil {
		return PreparedUpdate{}, err
	}
	// The sealed acquisition is also checked against the immutable plan; a
	// complete but unrelated run must not be compared under this task's window.
	for _, page := range l.Pages() {
		r := page.Request
		if string(r.Mode) != p.Mode {
			return PreparedUpdate{}, ErrInitialization
		}
		if r.Mode == ingest.Incremental {
			w, ok := p.Windows[r.Table]
			if !ok || r.StartTime != w.Start.In(f.Zone).Format("2006-01-02 15:04:05.999999") || r.EndTime != w.End.In(f.Zone).Format("2006-01-02 15:04:05.999999") {
				return PreparedUpdate{}, ErrInitialization
			}
		}
	}
	o := f.Options
	o.Mode = ingest.Mode(p.Mode)
	// Both update modes compare with the acknowledged ORIGINAL full baseline.
	// Without it a full fetch could bypass deletion/abnormal-empty protection.
	if o.Base == nil || o.BaseSource == nil || o.Base.Combination != p.Origin.Position.BundleID || o.Base.Validate() != nil {
		return PreparedUpdate{}, ErrInitialization
	}
	m, err := f.Preparer.Prepare(ctx, o, l)
	if err != nil {
		return PreparedUpdate{}, err
	}
	if m.Validate() != nil || m.Combination != o.Combination || m.Input != l.Reference() {
		return PreparedUpdate{}, ErrInitialization
	}
	counter, ok := f.Preparer.(ChangeCounter)
	if !ok {
		return PreparedUpdate{}, ErrInitialization
	}
	counts, err := counter.Changes(ctx, m)
	if err != nil {
		return PreparedUpdate{}, err
	}
	return PreparedUpdate{Plan: p, Manifest: m, Lease: l, Changes: counts}, nil
}
