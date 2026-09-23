package observability

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"sync/atomic"
	"syscall"
)

var ErrCapacity = errors.New("log buffer capacity exhausted")
var ErrClosed = errors.New("log buffer is closing")
var ErrIncomplete = errors.New("log shutdown incomplete; unacknowledged state retained")

type Usage struct{ Bytes, Files int64 }
type CapacityConfig struct {
	MaxBytes, MaxFiles int64
	WarningPercent     int
	Policy             string
}

const RejectNew = "reject_new"

type CapacityReport struct {
	Usage                Usage
	Warning, Unavailable bool
	Rejected             uint64
}
type quotaState struct {
	report      atomic.Pointer[CapacityReport]
	rejected    atomic.Uint64
	unavailable atomic.Bool
}

// One QuotaJournal must be shared by all buffers using this service directory.
// Temporary and orphan files count; neither dropping nor overwriting is allowed.
type QuotaJournal struct {
	files  Journal
	config CapacityConfig
	gate   chan struct{}
	state  *quotaState
}

func (q QuotaJournal) String() string   { return "[log quota redacted]" }
func (q QuotaJournal) GoString() string { return q.String() }
func NewQuota(files Journal, c CapacityConfig) (*QuotaJournal, error) {
	if files == nil || c.MaxBytes < 1024 || c.MaxBytes > 1<<40 || c.MaxFiles < 2 || c.MaxFiles > 1000000 || c.WarningPercent < 1 || c.WarningPercent > 99 || c.Policy != RejectNew {
		return nil, ErrCapacity
	}
	q := &QuotaJournal{files: files, config: c, gate: make(chan struct{}, 1), state: &quotaState{}}
	q.state.report.Store(&CapacityReport{})
	return q, nil
}
func (q *QuotaJournal) Status() CapacityReport {
	r := *q.state.report.Load()
	r.Rejected = q.state.rejected.Load()
	return r
}
func (q *QuotaJournal) sample(ctx context.Context) (Usage, error) {
	u, err := q.files.Usage(ctx)
	if err != nil || u.Bytes < 0 || u.Files < 0 {
		q.state.unavailable.Store(true)
		q.state.report.Store(&CapacityReport{Unavailable: true})
		return Usage{}, ErrBuffer
	}
	warning := u.Bytes >= (q.config.MaxBytes*int64(q.config.WarningPercent)+99)/100 || u.Files >= (q.config.MaxFiles*int64(q.config.WarningPercent)+99)/100
	q.state.report.Store(&CapacityReport{Usage: u, Warning: warning, Unavailable: q.state.unavailable.Load()})
	return u, nil
}
func (q *QuotaJournal) Put(ctx context.Context, id string, body []byte) error {
	select {
	case q.gate <- struct{}{}:
		defer func() { <-q.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	old, err := q.files.Lookup(ctx, id)
	if err == nil {
		if !bytes.Equal(old, body) {
			return ErrConflict
		}
		return q.write(ctx, id, body, q.Status().Usage)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		q.state.unavailable.Store(true)
		q.state.report.Store(&CapacityReport{Unavailable: true})
		return ErrBuffer
	}
	u, err := q.sample(ctx)
	if err != nil {
		return err
	}
	if u.Bytes > q.config.MaxBytes || u.Files > q.config.MaxFiles-2 || int64(len(body)) > (q.config.MaxBytes-u.Bytes)/2 {
		q.state.rejected.Add(1)
		r := q.Status()
		r.Warning = true
		q.state.report.Store(&r)
		return ErrCapacity
	}
	return q.write(ctx, id, body, u)
}
func (q *QuotaJournal) write(ctx context.Context, id string, body []byte, u Usage) error {
	if err := q.files.Put(ctx, id, body); err != nil {
		if errors.Is(err, syscall.ENOSPC) || errors.Is(err, ErrCapacity) {
			q.state.unavailable.Store(true)
			q.state.rejected.Add(1)
			r := q.Status()
			r.Warning = true
			r.Unavailable = true
			q.state.report.Store(&r)
			return ErrCapacity
		}
		q.state.unavailable.Store(true)
		q.state.report.Store(&CapacityReport{Usage: u, Unavailable: true})
		return ErrBuffer
	}
	q.state.unavailable.Store(false)
	_, err := q.sample(ctx)
	return err
}
func (q *QuotaJournal) Lookup(ctx context.Context, id string) ([]byte, error) {
	return q.files.Lookup(ctx, id)
}
func (q *QuotaJournal) Batch(ctx context.Context, n, b int) ([]Stored, error) {
	return q.files.Batch(ctx, n, b)
}
func (q *QuotaJournal) Remove(ctx context.Context, id string) error {
	select {
	case q.gate <- struct{}{}:
		defer func() { <-q.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := q.files.Remove(ctx, id); err != nil {
		return err
	}
	_, err := q.sample(ctx)
	return err
}
func (q *QuotaJournal) Flush(ctx context.Context) error          { return q.files.Flush(ctx) }
func (q *QuotaJournal) Usage(ctx context.Context) (Usage, error) { return q.sample(ctx) }
