package observability_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/lifecycle"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
)

func TestT05_17BlockedFlushUsesTotalLifecycleBudgetAndRetainsPending(t *testing.T) {
	j := newJournal()
	release := make(chan struct{})
	entered := make(chan struct{})
	returned := make(chan struct{})
	j.flush = func(context.Context) error { close(entered); <-release; close(returned); return nil }
	b := buffer(t, j)
	safe := appendEvent(t, b, 1)
	d := dispatcher(t, b, &logStore{}, &dispatchClock{now: event().OccurredAt})
	m := &lifecycle.Manager{}
	m.Add("logs", d)
	finished := make(chan []lifecycle.Result, 1)
	go func() { finished <- m.Close(30 * time.Millisecond) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("flush did not start")
	}
	select {
	case result := <-finished:
		if len(result) != 1 || result[0].State != "incomplete" {
			t.Fatal("blocked flush claimed closed")
		}
	case <-time.After(time.Second):
		t.Fatal("exit exceeded total budget")
	}
	if err := b.Append(context.Background(), safe); err != observability.ErrClosed {
		t.Fatal("shutdown admitted new events")
	}
	close(release)
	<-returned
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Close(ctx); err != observability.ErrIncomplete {
		t.Fatal("late flush fabricated successful shutdown")
	}
	if len(j.files) != 1 {
		t.Fatal("unacknowledged file lost during shutdown")
	}
}

type stubbornTimer struct {
	active  atomic.Int64
	started chan struct{}
	release chan struct{}
}

func (c *stubbornTimer) Now() time.Time { return event().OccurredAt }
func (c *stubbornTimer) Wait(context.Context, time.Duration) error {
	c.active.Add(1)
	defer c.active.Add(-1)
	select {
	case c.started <- struct{}{}:
	default:
	}
	<-c.release
	return nil
}
func TestShutdownAccountsForUncooperativeTimerAndDoesNotAccumulateWaiters(t *testing.T) {
	j := newJournal()
	b := buffer(t, j)
	timer := &stubbornTimer{started: make(chan struct{}, 1), release: make(chan struct{})}
	d, err := observability.NewDispatcher(b, &logStore{}, timer, observability.DispatchConfig{MaxWait: time.Minute, RetryWait: time.Second, Timeout: time.Second, BatchBytes: 32768})
	if err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- d.Run(context.Background()) }()
	select {
	case <-timer.started:
	case <-time.After(time.Second):
		t.Fatal("timer not started")
	}
	for i := 0; i < 5; i++ {
		appendEvent(t, b, i)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := d.Close(ctx); err != observability.ErrIncomplete {
		t.Fatal("blocked timer claimed drained")
	}
	if timer.active.Load() != 1 {
		t.Fatal("wake notifications accumulated timer goroutines")
	}
	close(timer.release)
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}
func TestGracefulCloseFlushesAndDeliversWithoutClaimingUnknownCommit(t *testing.T) {
	j := newJournal()
	b := buffer(t, j)
	appendEvent(t, b, 1)
	store := &logStore{}
	d := dispatcher(t, b, store, &dispatchClock{now: event().OccurredAt})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil || len(j.files) != 0 || len(store.rows) != 1 {
		t.Fatal("graceful close did not acknowledge backlog")
	}
}
