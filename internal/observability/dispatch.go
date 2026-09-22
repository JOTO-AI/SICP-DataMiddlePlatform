package observability

import (
	"context"
	"sync/atomic"
	"time"
)

type Timer interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}
type SystemTimer struct{}

func (SystemTimer) Now() time.Time { return time.Now() }
func (SystemTimer) Wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type DispatchConfig struct {
	MaxWait, RetryWait, Timeout time.Duration
	BatchBytes                  int
}
type DispatchStats struct{ Delivered, Failures uint64 }
type Dispatcher struct {
	buffer                   *Buffer
	store                    LogStore
	clock                    Timer
	config                   DispatchConfig
	gate                     chan struct{}
	firstPending, retryUntil time.Time
	delivered, failures      atomic.Uint64
}

func NewDispatcher(b *Buffer, store LogStore, clock Timer, c DispatchConfig) (*Dispatcher, error) {
	if b == nil || store == nil || clock == nil || c.MaxWait <= 0 || c.MaxWait > time.Minute || c.RetryWait <= 0 || c.RetryWait > time.Minute || c.Timeout <= 0 || c.Timeout > time.Minute || c.BatchBytes < 1 || c.BatchBytes > b.batchBytes {
		return nil, ErrBuffer
	}
	return &Dispatcher{buffer: b, store: store, clock: clock, config: c, gate: make(chan struct{}, 1)}, nil
}
func (d *Dispatcher) Stats() DispatchStats {
	return DispatchStats{d.delivered.Load(), d.failures.Load()}
}

// Pump performs at most one batch. Run owns its timer; tests drive the identical
// decision with FakeClock. Failures use counters, never recursive event logs.
func (d *Dispatcher) Pump(ctx context.Context) (time.Duration, error) {
	select {
	case d.gate <- struct{}{}:
		defer func() { <-d.gate }()
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	now := d.clock.Now()
	if now.Before(d.retryUntil) {
		return d.retryUntil.Sub(now), nil
	}
	batch, err := d.buffer.Pending(ctx)
	if err != nil {
		return d.failed(d.clock.Now(), err)
	}
	if len(batch) == 0 {
		d.firstPending = time.Time{}
		return d.config.MaxWait, nil
	}
	if d.firstPending.IsZero() {
		d.firstPending = now
		for _, e := range batch {
			if e.Event().OccurredAt.Before(d.firstPending) {
				d.firstPending = e.Event().OccurredAt
			}
		}
	}
	size := 0
	for _, e := range batch {
		size += len(e.body)
	}
	deadline := d.firstPending.Add(d.config.MaxWait)
	if len(batch) < d.buffer.batchCount && size < d.config.BatchBytes && now.Before(deadline) {
		return deadline.Sub(now), nil
	}
	callCtx, cancel := context.WithTimeout(ctx, d.config.Timeout)
	count, err := d.buffer.Deliver(callCtx, d.store)
	cancel()
	d.delivered.Add(uint64(count))
	if err != nil {
		return d.failed(d.clock.Now(), err)
	}
	d.firstPending = time.Time{}
	d.retryUntil = time.Time{}
	return 0, nil
}
func (d *Dispatcher) failed(now time.Time, err error) (time.Duration, error) {
	d.failures.Add(1)
	d.retryUntil = now.Add(d.config.RetryWait)
	return d.config.RetryWait, err
}
func (d *Dispatcher) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		delay, _ := d.Pump(ctx)
		if delay <= 0 {
			continue
		}
		waitCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- d.clock.Wait(waitCtx, delay) }()
		select {
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		case <-d.buffer.wake:
			cancel()
		case err := <-done:
			cancel()
			if err != nil && ctx.Err() == nil {
				return ErrBuffer
			}
		}
	}
}
