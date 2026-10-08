package control

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// BeginFenced holds the execution fence and coordinator locks through a runtime
// adapter operation. The caller must finish all physical SQL before releasing
// the transaction. A timeout alone is never evidence of physical completion.
func (r Repository) BeginFenced(ctx context.Context, positions ...storage.Position) (storage.Transaction, error) {
	if r.owner == nil || r.Database == nil || r.Registry.Control().DSN() == "" {
		return nil, ErrOwnership
	}
	tx, e := r.Database.Begin(ctx, r.Registry.Control())
	if e != nil {
		return nil, e
	}
	fail := func(e error) (storage.Transaction, error) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
		return nil, e
	}
	if e = r.fenceWriter(ctx, tx); e != nil {
		return fail(e)
	}
	if len(positions) == 0 {
		if e = lockSnapshots(ctx, tx); e != nil {
			return fail(e)
		}
		for _, i := range []storage.Instance{storage.Main, storage.Standby} {
			for s := 0; s < 2; s++ {
				positions = append(positions, storage.Position{Instance: i, Slot: s})
			}
		}
	}
	for _, p := range positions {
		if e = lockSlot(ctx, tx, p); e != nil {
			return fail(e)
		}
	}
	if buildmetric.Enabled(ctx) {
		return &measuredFence{Transaction: tx, ctx: ctx, started: time.Now()}, nil
	}
	return tx, nil
}

type measuredFence struct {
	storage.Transaction
	ctx     context.Context
	started time.Time
	once    sync.Once
}

func (t *measuredFence) ended() {
	t.once.Do(func() { buildmetric.Add(t.ctx, buildmetric.LockHeldNS, time.Since(t.started).Nanoseconds()) })
}
func (t *measuredFence) Commit(ctx context.Context) error {
	err := t.Transaction.Commit(ctx)
	if err == nil {
		t.ended()
	}
	return err
}
func (t *measuredFence) Rollback(ctx context.Context) error {
	err := t.Transaction.Rollback(ctx)
	if err == nil {
		t.ended()
	}
	return err
}
func (r Repository) Fenced(ctx context.Context, fn func(storage.Transaction) error) error {
	if fn == nil {
		return ErrEntity
	}
	tx, e := r.BeginFenced(ctx)
	if e != nil {
		return e
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if e = fn(tx); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// ReadFact shares the existing allowlisted mapping with runtime adapters that
// already hold a transaction. It cannot choose an arbitrary table or identifier.
func ReadFact(ctx context.Context, tx storage.Transaction, entity any) error {
	d, e := describe(entity)
	if e != nil {
		return e
	}
	pred, args, e := keys(d, 0)
	if e != nil {
		return e
	}
	v, e := readEntity(ctx, tx, d, strings.Join(pred, " AND "), args, true)
	if e != nil {
		return e
	}
	d.value.Set(v)
	return nil
}
