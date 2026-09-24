package update

import (
	"context"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type UnchangedCheck interface {
	Try(context.Context, PreparedUpdate) (bool, error)
}
type Runner struct {
	Source    UpdateSource
	Unchanged UnchangedCheck
	Changed   Changed
}
type Result struct {
	Unchanged bool
	Change    ChangeResult
}

// Run acquires exactly once and branches on verified complete source content.
func (r Runner) Run(ctx context.Context, active control.ActiveMetadata) (Result, error) {
	if r.Source == nil || r.Unchanged == nil {
		return Result{}, ErrInitialization
	}
	in, err := r.Source.Prepare(ctx, active)
	if err != nil {
		return Result{}, err
	}
	same, err := r.Unchanged.Try(ctx, in)
	if err != nil {
		return Result{}, err
	}
	if same {
		return Result{Unchanged: true, Change: ChangeResult{Prepared: in}}, nil
	}
	out, err := r.Changed.RunPrepared(ctx, in)
	return Result{Change: out}, err
}
