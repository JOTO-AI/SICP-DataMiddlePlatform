// Package read defines pinned query and actual SQL lifetime boundaries.
package read

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrHandle = errors.New("invalid or unavailable read handle")

type Handle struct {
	Position    storage.Position
	Generation  int64
	Combination string
}

func (h Handle) Validate() error {
	if h.Position.Validate() != nil || h.Generation < 1 || strings.TrimSpace(h.Combination) == "" || len(h.Combination) > 256 {
		return ErrHandle
	}
	return nil
}

// Admission selects a legal combination ONCE per HTTP request. A requested
// snapshot is a transport credential, never a user-supplied physical position.
type Admission interface {
	Acquire(context.Context, string) (RequestLease, error)
}

type RequestLease interface {
	Handle() Handle
	Close(context.Context) error
}

// Gate atomically validates generation/combination and registers actual SQL.
// A request timeout or expired request lease does not release an SQLLease.
// The registration prevents recycling the physical slot until Release.
type Gate interface {
	BeginRead(context.Context, Handle) (SQLLease, error)
}

type SQLLease interface {
	Handle() Handle
	Release(context.Context) error
}

type BatchReader interface {
	ReadBatch(context.Context, storage.Position, string, string, int) ([]source.SourceRow, error)
}

type Repository struct {
	Storage BatchReader
	Gate    Gate
}

// ReadBatch never consults an active route. It uses the complete caller handle
// at admission and its position for the underlying fixed-slot repository.
func (r Repository) ReadBatch(ctx context.Context, h Handle, table, after string, limit int) (rows []source.SourceRow, err error) {
	return r.withRead(ctx, h, func() ([]source.SourceRow, error) { return r.Storage.ReadBatch(ctx, h.Position, table, after, limit) })
}

type MatchingReader interface {
	ReadMatching(context.Context, storage.Position, string, string, int, []source.Filter) ([]source.SourceRow, error)
}

func (r Repository) ReadMatching(ctx context.Context, h Handle, table, after string, limit int, filters []source.Filter) ([]source.SourceRow, error) {
	reader, ok := r.Storage.(MatchingReader)
	if !ok {
		return nil, ErrHandle
	}
	return r.withRead(ctx, h, func() ([]source.SourceRow, error) {
		return reader.ReadMatching(ctx, h.Position, table, after, limit, filters)
	})
}

func (r Repository) withRead(ctx context.Context, h Handle, run func() ([]source.SourceRow, error)) (rows []source.SourceRow, err error) {
	if h.Validate() != nil || r.Gate == nil || r.Storage == nil {
		return nil, ErrHandle
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	lease, err := r.Gate.BeginRead(ctx, h)
	if err != nil {
		return nil, err
	}
	if lease == nil {
		return nil, ErrHandle
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if releaseErr := lease.Release(cleanup); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		if err != nil {
			rows = nil
		}
	}()
	if lease.Handle() != h {
		return nil, ErrHandle
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	rows, err = run()
	if err == nil {
		err = ctx.Err()
	}
	return rows, err
}

// Execute holds the selected request lease through the complete use case. The
// callback must synchronously finish its reads before returning; HTTP/cache
// integration is supplied by the business handlers and P11 pipeline.
func Execute(ctx context.Context, admission Admission, snapshot string, run func(context.Context, Handle) (any, error)) (result any, err error) {
	if admission == nil || run == nil {
		return nil, ErrHandle
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	lease, err := admission.Acquire(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	if lease == nil {
		return nil, ErrHandle
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if closeErr := lease.Close(cleanup); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		if err != nil {
			result = nil
		}
	}()
	h := lease.Handle()
	if h.Validate() != nil {
		return nil, ErrHandle
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	result, err = run(ctx, h)
	if err == nil {
		err = ctx.Err()
	}
	return result, err
}
