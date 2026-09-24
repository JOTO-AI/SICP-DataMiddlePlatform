package coord

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type ReaderStore interface {
	RegisterReader(context.Context, control.SlotIdentity, string, string, time.Time) (control.Reader, error)
	ReleaseReader(context.Context, control.SlotIdentity, string, time.Time) error
	BeginSQL(context.Context, control.SlotIdentity, string, string, time.Time) error
	FinishSQL(context.Context, control.SlotIdentity, string, string, time.Time) error
}
type Resolver interface {
	Resolve(context.Context, string) (read.Handle, error)
}

// ExecutionProof must confirm the actual database execution ended. Deadline,
// process liveness and heartbeats alone cannot implement this interface.
type ExecutionProof interface {
	Stopped(context.Context, string) (bool, error)
}
type Coordinator struct {
	Store     ReaderStore
	Resolver  Resolver
	Proof     ExecutionProof
	Clock     ports.Clock
	Random    ports.Random
	ProcessID string
}
type requestKey struct{}
type sqlKey struct{}
type requestLease struct {
	owner *Coordinator
	h     read.Handle
	id    string
}
type sqlLease struct {
	owner        *Coordinator
	h            read.Handle
	readerID, id string
}

func identity(h read.Handle) control.SlotIdentity {
	return control.SlotIdentity{Position: h.Position, Generation: h.Generation, BundleID: h.Combination}
}
func (c *Coordinator) id() (string, error) {
	var data [16]byte
	if _, err := io.ReadFull(c.Random, data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func (c *Coordinator) Acquire(ctx context.Context, snapshot string) (read.RequestLease, error) {
	if c == nil || c.Store == nil || c.Resolver == nil || c.Proof == nil || c.Clock == nil || c.Random == nil || c.ProcessID == "" {
		return nil, read.ErrHandle
	}
	h, err := c.Resolver.Resolve(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	return c.AcquireFixed(ctx, h)
}

// AcquireFixed registers the already selected combination. Cache work must not
// resolve a new active route after its key has been chosen.
func (c *Coordinator) AcquireFixed(ctx context.Context, h read.Handle) (read.RequestLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || c.Store == nil || c.Proof == nil || c.Clock == nil || c.Random == nil || c.ProcessID == "" {
		return nil, read.ErrHandle
	}
	if h.Validate() != nil {
		return nil, read.ErrHandle
	}
	id, err := c.id()
	if err != nil {
		return nil, err
	}
	if _, err = c.Store.RegisterReader(ctx, identity(h), id, c.ProcessID, c.Clock.Now()); err != nil {
		return nil, err
	}
	return &requestLease{owner: c, h: h, id: id}, nil
}
func (l *requestLease) Handle() read.Handle { return l.h }
func (l *requestLease) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestKey{}, l)
}
func (l *requestLease) Close(ctx context.Context) error {
	return l.owner.Store.ReleaseReader(ctx, identity(l.h), l.id, l.owner.Clock.Now())
}

func (c *Coordinator) BeginRead(ctx context.Context, h read.Handle) (read.SQLLease, error) {
	l, ok := ctx.Value(requestKey{}).(*requestLease)
	if !ok || l.owner != c || l.h != h {
		return nil, read.ErrHandle
	}
	id, err := c.id()
	if err != nil {
		return nil, err
	}
	if err = c.Store.BeginSQL(ctx, identity(h), l.id, id, c.Clock.Now()); err != nil {
		return nil, err
	}
	return &sqlLease{owner: c, h: h, readerID: l.id, id: id}, nil
}
func (l *sqlLease) Handle() read.Handle { return l.h }
func (l *sqlLease) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, sqlKey{}, l.id)
}
func (l *sqlLease) Release(ctx context.Context) error {
	stopped, err := l.owner.Proof.Stopped(ctx, l.id)
	if err != nil {
		return err
	}
	if !stopped {
		return errors.New("SQL execution termination unconfirmed")
	}
	if err := l.owner.Store.FinishSQL(ctx, identity(l.h), l.readerID, l.id, l.owner.Clock.Now()); err != nil {
		return err
	}
	if proof, ok := l.owner.Proof.(interface{ Forget(string) }); ok {
		proof.Forget(l.id)
	}
	return nil
}

// ExecutionID lets the database adapter associate a durable execution with its
// session/transaction termination evidence without exposing it to HTTP clients.
func ExecutionID(ctx context.Context) string { id, _ := ctx.Value(sqlKey{}).(string); return id }
