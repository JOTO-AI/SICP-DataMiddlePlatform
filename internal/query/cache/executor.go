package cache

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
)

// Executor admits cache hits only after the pinned combination is confirmed.
// Validate must consult confirmed control state, never a speculative route.
type Executor struct {
	Results  Results
	Flights  *Coalescer
	Validate func(context.Context, read.Handle) error
}

func (e Executor) Query(ctx context.Context, h read.Handle, op contract.Operation, input any, load func(context.Context, read.Handle) ([]byte, error)) (Result, error) {
	if h.Validate() != nil || e.Validate == nil || e.Flights == nil || e.Results.Store == nil || e.Results.Observe == nil || load == nil {
		return Result{}, read.ErrHandle
	}
	key, err := Key(h.Combination, op, input)
	if err != nil {
		return Result{}, err
	}
	if err = e.Validate(ctx, h); err != nil {
		return Result{}, err
	}
	cached, err := e.Results.Lookup(ctx, key)
	if err == nil && cached.Hit {
		return cached, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		e.Results.Observe(err)
	}
	return e.Flights.Do(ctx, key, func(workCtx context.Context) (Result, error) {
		if err := e.Validate(workCtx, h); err != nil {
			return Result{}, err
		}
		// Another request may have filled the key before this execution started.
		cached, err := e.Results.Lookup(workCtx, key)
		if err == nil && cached.Hit {
			return cached, nil
		}
		if err != nil {
			if workCtx.Err() != nil {
				return Result{}, workCtx.Err()
			}
			e.Results.Observe(err)
		}
		data, err := load(workCtx, h)
		if err != nil {
			return Result{}, err
		}
		if err = workCtx.Err(); err != nil {
			return Result{}, err
		}
		if !validPayload(data) {
			return Result{}, ErrPayload
		}
		result := cloneResult(Result{Data: data})
		if err = e.Results.Fill(workCtx, key, data); err != nil {
			e.Results.Observe(err)
		}
		return result, nil
	})
}
