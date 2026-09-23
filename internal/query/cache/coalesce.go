package cache

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"
)

var ErrCapacity = errors.New("query execution capacity exhausted")

type Work func(context.Context) (Result, error)
type flight struct {
	done     chan struct{}
	cancel   context.CancelFunc
	waiters  int
	finished bool
	result   Result
	err      error
}

// Coalescer is per query process. Work owns its own read lease and finishes all
// actual SQL before returning, even when its context has been canceled.
type Coalescer struct {
	mu                         sync.Mutex
	jobs                       map[string]*flight
	semaphore                  chan struct{}
	capacity, waiting, running int
	limit                      time.Duration
	closed                     bool
	done                       chan struct{}
}

func NewCoalescer(executions, queue int, limit time.Duration) (*Coalescer, error) {
	if executions < 1 || executions > 10000 || queue < 0 || queue > 100000 || limit <= 0 || limit > time.Hour {
		return nil, ErrCapacity
	}
	return &Coalescer{jobs: map[string]*flight{}, semaphore: make(chan struct{}, executions), capacity: executions + queue, limit: limit, done: make(chan struct{})}, nil
}
func cloneResult(r Result) Result { return Result{Data: bytes.Clone(r.Data), Hit: r.Hit} }

func (c *Coalescer) Do(ctx context.Context, key string, work Work) (Result, error) {
	if c == nil || key == "" || work == nil {
		return Result{}, ErrCapacity
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	c.mu.Lock()
	if c.closed || c.waiting >= c.capacity {
		c.mu.Unlock()
		return Result{}, ErrCapacity
	}
	f := c.jobs[key]
	if f != nil && f.waiters == 0 {
		c.mu.Unlock()
		return Result{}, ErrCapacity
	}
	if f == nil {
		if len(c.jobs) >= c.capacity {
			c.mu.Unlock()
			return Result{}, ErrCapacity
		}
		workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.limit)
		f = &flight{done: make(chan struct{}), cancel: cancel}
		c.jobs[key] = f
		go c.execute(workCtx, key, f, work)
	}
	f.waiters++
	c.waiting++
	c.mu.Unlock()
	defer c.leave(f)
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-f.done:
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		return cloneResult(f.result), f.err
	}
}
func (c *Coalescer) leave(f *flight) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f.waiters--
	c.waiting--
	if f.waiters == 0 && !f.finished {
		f.cancel()
	}
}
func (c *Coalescer) execute(ctx context.Context, key string, f *flight, work Work) {
	var result Result
	var err error
	select {
	case c.semaphore <- struct{}{}:
		c.mu.Lock()
		c.running++
		c.mu.Unlock()
		if err = ctx.Err(); err == nil {
			result, err = work(ctx)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		<-c.semaphore
		c.mu.Lock()
		c.running--
		c.mu.Unlock()
	case <-ctx.Done():
		err = ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		result = Result{}
	}
	f.result, f.err, f.finished = cloneResult(result), err, true
	delete(c.jobs, key)
	f.cancel()
	close(f.done)
	if c.closed && len(c.jobs) == 0 {
		close(c.done)
	}
}

// State is a bounded resource diagnostic; waiting includes executing callers.
func (c *Coalescer) State() (running, waiting, flights int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running, c.waiting, len(c.jobs)
}
func (c *Coalescer) Close(ctx context.Context) error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		for _, f := range c.jobs {
			f.cancel()
		}
		if len(c.jobs) == 0 {
			close(c.done)
		}
	}
	c.mu.Unlock()
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
