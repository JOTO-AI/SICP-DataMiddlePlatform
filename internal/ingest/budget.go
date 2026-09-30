package ingest

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
)

const (
	BudgetFailure          FailureKind = "budget"
	RequestTimeout         FailureKind = "request_timeout"
	CooldownFailure        FailureKind = "cooldown"
	CooldownStorageFailure FailureKind = "cooldown_storage"
)

type Timer interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}
type SystemTimer struct{}

func (SystemTimer) Now() time.Time { return time.Now() }
func (SystemTimer) Wait(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type CooldownStore interface {
	Cooldown(context.Context) (time.Time, error)
	ExtendCooldown(context.Context, time.Time) error
}

// Admission must be shared by all calls to this upstream. Its context covers
// queue time; release must retain the permit until the actual call has ended.
type Admission interface {
	Acquire(context.Context) (func(), error)
}

type Limiter struct{ permits chan struct{} }

func NewLimiter(concurrency int) (*Limiter, error) {
	if concurrency < 1 || concurrency > 100 {
		return nil, &Failure{Kind: ConfigurationFailure}
	}
	return &Limiter{permits: make(chan struct{}, concurrency)}, nil
}
func (l *Limiter) Acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case l.permits <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-l.permits }) }, nil
	}
}

type TaskBudget struct {
	deadline        time.Time
	maxCalls, calls int
	mu              sync.Mutex
}

func NewTaskBudget(deadline time.Time, maxCalls int) (*TaskBudget, error) {
	if deadline.IsZero() || maxCalls < 1 {
		return nil, &Failure{Kind: ConfigurationFailure}
	}
	return &TaskBudget{deadline: deadline, maxCalls: maxCalls}, nil
}
func (b *TaskBudget) consume() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.calls >= b.maxCalls {
		return false
	}
	b.calls++
	return true
}
func (b *TaskBudget) Calls() int { b.mu.Lock(); defer b.mu.Unlock(); return b.calls }

type BudgetConfig struct {
	RequestTimeout, PageTimeout time.Duration
	InitialBackoff, MaxBackoff  time.Duration
	MaxAttempts                 int
	// Explicitly confirmed transient HTTP statuses only. Business failure codes
	// remain deterministic until a provider contract defines their meaning.
	RetryHTTP []int
}
type Coordinator struct {
	client    *Client
	config    BudgetConfig
	clock     Timer
	random    ports.Random
	cooldown  CooldownStore
	admission Admission
	randomMu  sync.Mutex
}

func NewCoordinator(client *Client, config BudgetConfig, clock Timer, random ports.Random, cooldown CooldownStore, admission Admission) (*Coordinator, error) {
	if client == nil || clock == nil || random == nil || cooldown == nil || admission == nil ||
		config.RequestTimeout <= 0 || config.PageTimeout <= 0 || config.InitialBackoff <= 0 ||
		config.MaxBackoff < config.InitialBackoff || config.MaxAttempts < 1 || config.MaxAttempts > 100 {
		return nil, &Failure{Kind: ConfigurationFailure}
	}
	for _, status := range config.RetryHTTP {
		switch status {
		case 408, 429, 500, 502, 503, 504:
		default:
			return nil, &Failure{Kind: ConfigurationFailure}
		}
	}
	config.RetryHTTP = append([]int(nil), config.RetryHTTP...)
	return &Coordinator{client: client, config: config, clock: clock, random: random, cooldown: cooldown, admission: admission}, nil
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func (c *Coordinator) expired(ctx context.Context, deadline time.Time) bool {
	return ctx.Err() != nil || !c.clock.Now().Before(deadline)
}

func (c *Coordinator) FetchPage(ctx context.Context, input PageRequest, task *TaskBudget) (PageResult, error) {
	if task == nil {
		return PageResult{}, &Failure{Kind: ConfigurationFailure}
	}
	if err := ctx.Err(); err != nil {
		return PageResult{}, &Failure{Kind: CancelledFailure, Cause: err}
	}
	deadline := earlier(c.clock.Now().Add(c.config.PageTimeout), task.deadline)
	if outer, ok := ctx.Deadline(); ok {
		deadline = earlier(deadline, outer)
	}
	pageCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	policy := RetryPolicy{MaxAttempts: c.config.MaxAttempts, Wait: func(ctx context.Context, n int) error {
		if c.expired(ctx, deadline) {
			return &Failure{Kind: BudgetFailure}
		}
		delay := c.config.InitialBackoff
		for i := 1; i < n && delay < c.config.MaxBackoff; i++ {
			if delay > c.config.MaxBackoff/2 {
				delay = c.config.MaxBackoff
			} else {
				delay *= 2
			}
		}
		var random [8]byte
		c.randomMu.Lock()
		nread, readErr := c.random.Read(random[:])
		c.randomMu.Unlock()
		if readErr != nil || nread != len(random) {
			return &Failure{Kind: RetryWaitFailure}
		}
		// Jitter remains within [half, full] of the capped exponential delay.
		half := delay / 2
		delay = half + time.Duration(binary.LittleEndian.Uint64(random[:])%uint64(delay-half+1))
		if delay <= 0 {
			delay = time.Nanosecond
		}
		if !c.clock.Now().Add(delay).Before(deadline) {
			return &Failure{Kind: BudgetFailure}
		}
		if err := c.clock.Wait(ctx, delay); err != nil {
			return &Failure{Kind: BudgetFailure}
		}
		if c.expired(ctx, deadline) {
			return &Failure{Kind: BudgetFailure}
		}
		return nil
	}}
	return fetchPage(pageCtx, input, policy, func(ctx context.Context, r PageRequest) (Page, error) { return c.attempt(ctx, r, task, deadline) })
}

func (c *Coordinator) attempt(ctx context.Context, input PageRequest, task *TaskBudget, pageDeadline time.Time) (Page, error) {
	if c.expired(ctx, pageDeadline) {
		return Page{}, &Failure{Kind: BudgetFailure}
	}
	deadline := earlier(c.clock.Now().Add(c.config.RequestTimeout), pageDeadline)
	attemptCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	release, err := c.admission.Acquire(attemptCtx)
	if err != nil {
		return Page{}, &Failure{Kind: BudgetFailure}
	}
	if release == nil {
		return Page{}, &Failure{Kind: ConfigurationFailure}
	}
	defer release()
	for {
		if c.expired(attemptCtx, deadline) {
			return Page{}, &Failure{Kind: BudgetFailure}
		}
		until, err := c.cooldown.Cooldown(attemptCtx)
		if err != nil {
			return Page{}, &Failure{Kind: CooldownStorageFailure}
		}
		if !until.After(c.clock.Now()) {
			break
		}
		if !until.Before(deadline) {
			return Page{}, &Failure{Kind: CooldownFailure}
		}
		if err := c.clock.Wait(attemptCtx, until.Sub(c.clock.Now())); err != nil {
			return Page{}, &Failure{Kind: BudgetFailure}
		}
	}
	if c.expired(attemptCtx, deadline) || !task.consume() {
		return Page{}, &Failure{Kind: BudgetFailure}
	}
	page, err := c.client.Fetch(attemptCtx, input)
	var f *Failure
	var cause error
	meta, retryAfter := page.Attempt, page.retryAfter
	if errors.As(err, &f) {
		meta, retryAfter = f.Attempt, f.retryAfter
		cause = f.Cause
	}
	if retryAfter != "" {
		until, valid := retryAfterTime(retryAfter, c.clock.Now())
		if !valid {
			return Page{}, &Failure{Kind: CooldownFailure, Attempt: meta, Cause: cause}
		}
		// Persist even if the request/task has just expired. A bounded independent
		// control write prevents a later task from bypassing upstream cooldown.
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		saveErr := c.cooldown.ExtendCooldown(persist, until)
		cancel()
		if saveErr != nil {
			return Page{}, &Failure{Kind: CooldownStorageFailure, Attempt: meta, Cause: cause}
		}
		if !until.Before(pageDeadline) {
			return Page{}, &Failure{Kind: CooldownFailure, Attempt: meta, Cause: cause}
		}
	}
	if c.expired(ctx, pageDeadline) {
		return Page{}, &Failure{Kind: BudgetFailure, Attempt: meta, Cause: cause}
	}
	if c.expired(attemptCtx, deadline) {
		return Page{}, &Failure{Kind: RequestTimeout, Attempt: meta, Cause: cause, retryable: true}
	}
	if f != nil && f.Kind == HTTPFailure && f.Attempt.HTTPStatus != nil {
		for _, status := range c.config.RetryHTTP {
			if status == *f.Attempt.HTTPStatus {
				f.retryable = true
			}
		}
	}
	return page, err
}

func retryAfterTime(raw string, now time.Time) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if seconds, err := strconv.ParseUint(s, 10, 64); err == nil {
		if seconds > uint64(math.MaxInt64/int64(time.Second)) {
			return time.Time{}, false
		}
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	until, err := http.ParseTime(s)
	if err != nil {
		return time.Time{}, false
	}
	if until.Before(now) {
		until = now
	}
	return until, true
}
