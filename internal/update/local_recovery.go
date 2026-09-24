package update

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

var ErrReacquire = errors.New("sealed acquisition unusable; create a linked new task")

type LocalState interface {
	LoadLocal(context.Context, control.Task) (ChangeResult, error)
}
type SealedRecovery interface {
	Retain(context.Context, staging.SealRef, string) (*staging.Lease, error)
}
type LocalRecovery struct {
	Store   LocalState
	Files   SealedRecovery
	Targets candidate.CompleteSource
	Changed Changed
	Zone    *time.Location
}

func sameManifest(a, b candidate.Manifest) bool { return reflect.DeepEqual(a, b) }

// Run cannot fetch upstream pages. A failed acquisition or corrupt seal returns
// ErrReacquire before building; the caller must create a distinct task/run ID.
func (r LocalRecovery) Run(ctx context.Context, task control.Task) (ChangeResult, error) {
	if r.Store == nil || r.Files == nil || r.Targets == nil || r.Zone == nil {
		return ChangeResult{}, ErrInitialization
	}
	if task.Status == "failed" {
		return ChangeResult{}, ErrReacquire
	}
	if task.StartedAt == nil || task.Status != "running" {
		return ChangeResult{}, ErrRecoveryPending
	}
	saved, err := r.Store.LoadLocal(ctx, task)
	if err != nil {
		return ChangeResult{}, err
	}
	p := saved.Prepared.Plan
	m := saved.Prepared.Manifest
	if p.Validate() != nil || m.Validate() != nil || p.TaskID != task.ID || p.Mode != task.Mode || !p.StartedAt.Equal(*task.StartedAt) {
		return ChangeResult{}, ErrRecoveryPending
	}
	var roles control.UpdatePlan
	if task.Roles == nil || json.Unmarshal([]byte(*task.Roles), &roles) != nil || !reflect.DeepEqual(roles, p) {
		return ChangeResult{}, ErrRecoveryPending
	}
	lease, err := r.Files.Retain(ctx, m.Input, task.ID)
	if err != nil {
		return ChangeResult{}, errors.Join(ErrReacquire, err)
	}
	if err = lease.Validate(ctx); err != nil {
		return ChangeResult{}, errors.Join(ErrReacquire, err)
	}
	for _, page := range lease.Pages() {
		req := page.Request
		if string(req.Mode) != p.Mode {
			return ChangeResult{}, ErrReacquire
		}
		if req.Mode == ingest.Incremental {
			w, ok := p.Windows[req.Table]
			if !ok || req.StartTime != w.Start.In(r.Zone).Format("2006-01-02 15:04:05.999999") || req.EndTime != w.End.In(r.Zone).Format("2006-01-02 15:04:05.999999") {
				return ChangeResult{}, ErrReacquire
			}
		}
	}
	if err = r.Targets.Validate(ctx, m); err != nil {
		return ChangeResult{}, errors.Join(ErrReacquire, err)
	}
	saved.Prepared.Lease = lease
	return r.Changed.Resume(ctx, saved)
}
