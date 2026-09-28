package runtimecontrol

import (
	"context"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type Routes struct {
	Registry     storage.Registry
	Database     storage.Database
	ActiveReader update.ActiveReader
}

func (r Routes) transaction(ctx context.Context, fn func(storage.Transaction) error) error {
	tx, e := r.Database.Begin(ctx, r.Registry.Control())
	if e != nil {
		return e
	}
	defer rollback(ctx, tx)
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func readable(ctx context.Context, tx storage.Transaction, h read.Handle) error {
	var yes bool
	e := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.slots s JOIN control.bundles b ON b.id=s.bundle_id JOIN control.candidates c ON c.instance=s.instance AND c.slot=s.slot AND c.generation=s.generation AND c.bundle_id=s.bundle_id WHERE s.instance=$1 AND s.slot=$2 AND s.generation=$3 AND s.bundle_id=$4 AND s.state='ready' AND s.admission_open=TRUE AND b.published_at IS NOT NULL AND c.state='ready' AND c.ready_at IS NOT NULL AND c.evidence IS NOT NULL)", string(h.Position.Instance), int16(h.Position.Slot), h.Generation, h.Combination).Scan(&yes)
	if e != nil {
		return e
	}
	if !yes {
		return read.ErrHandle
	}
	return nil
}
func (r Routes) Validate(ctx context.Context, h read.Handle) error {
	if h.Validate() != nil {
		return read.ErrHandle
	}
	return r.transaction(ctx, func(tx storage.Transaction) error { return readable(ctx, tx, h) })
}
func (r Routes) Active(ctx context.Context) (read.Handle, error) {
	a, exists, e := r.ActiveReader.Read(ctx)
	if e != nil {
		return read.Handle{}, e
	}
	if !exists {
		return read.Handle{}, read.ErrHandle
	}
	h := read.Handle{Position: a.Position.Position, Generation: a.Position.Generation, Combination: a.Position.BundleID}
	e = r.transaction(ctx, func(tx storage.Transaction) error {
		confirmed, e := current(ctx, tx)
		if e != nil {
			return e
		}
		if confirmed == nil || *confirmed != a {
			return read.ErrHandle
		}
		return readable(ctx, tx, h)
	})
	if e != nil {
		return read.Handle{}, e
	}
	return h, nil
}
func (r Routes) ForCombination(ctx context.Context, id string) (h read.Handle, err error) {
	if id == "" || len(id) > 256 {
		return h, read.ErrHandle
	}
	err = r.transaction(ctx, func(tx storage.Transaction) error {
		// Prefer the current physical route for same-combination continuation.
		a, e := current(ctx, tx)
		if e != nil {
			return e
		}
		if a != nil && a.Position.BundleID == id {
			h = read.Handle{Position: a.Position.Position, Generation: a.Position.Generation, Combination: id}
			if e = readable(ctx, tx, h); e == nil {
				return nil
			} else if !errors.Is(e, read.ErrHandle) {
				return e
			}
		}
		for _, i := range []storage.Instance{storage.Main, storage.Standby} {
			for n := 0; n < 2; n++ {
				var generation *int64
				if e = tx.QueryRow(ctx, "SELECT (SELECT generation FROM control.slots WHERE instance=$1 AND slot=$2 AND bundle_id=$3 AND state='ready' AND admission_open=TRUE)", string(i), int16(n), id).Scan(&generation); e != nil {
					return e
				}
				if generation != nil {
					h = read.Handle{Position: storage.Position{Instance: i, Slot: n}, Generation: *generation, Combination: id}
					if e = readable(ctx, tx, h); e == nil {
						return nil
					} else if !errors.Is(e, read.ErrHandle) {
						return e
					}
				}
			}
		}
		return read.ErrHandle
	})
	if err != nil {
		return read.Handle{}, err
	}
	return
}
func (r Routes) Resolve(ctx context.Context, token string) (read.Handle, error) {
	if token != "" {
		return read.Handle{}, read.ErrHandle
	}
	return r.Active(ctx)
}
func (r Routes) Ready(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, e := r.Active(ctx)
	return e == nil
}
