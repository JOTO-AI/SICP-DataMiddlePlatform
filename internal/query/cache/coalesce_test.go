package cache_test

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
)

func waitState(t *testing.T, c *cache.Coalescer, waiting, flights int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		_, w, f := c.State()
		if w == waiting && f == flights {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("state waiting=%d flights=%d, wanted %d %d", w, f, waiting, flights)
		default:
			runtime.Gosched()
		}
	}
}
func group(t *testing.T, executions, queue int) *cache.Coalescer {
	t.Helper()
	c, err := cache.NewCoalescer(executions, queue, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestT11_07CanceledLeaderDoesNotCancelOtherWaiter(t *testing.T) {
	c := group(t, 1, 2)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	work := func(ctx context.Context) (cache.Result, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return cache.Result{Data: []byte(`[]`)}, nil
		case <-ctx.Done():
			return cache.Result{}, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { _, err := c.Do(ctx, "same", work); first <- err }()
	<-started
	go func() {
		result, err := c.Do(t.Context(), "same", work)
		if err == nil && string(result.Data) != "[]" {
			err = errors.New("wrong result")
		}
		second <- err
	}()
	waitState(t, c, 2, 1)
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("did not coalesce", calls.Load())
	}
	if err := c.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestT11_07QueueBoundAndCanceledExecutionKeepActualSlotOccupied(t *testing.T) {
	c := group(t, 1, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	second := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_, err := c.Do(ctx, "running", func(context.Context) (cache.Result, error) {
			close(started)
			<-release
			return cache.Result{Data: []byte(`[]`)}, nil
		})
		first <- err
	}()
	<-started
	queued, cancelQueued := context.WithCancel(t.Context())
	defer cancelQueued()
	var queuedCalls atomic.Int32
	go func() {
		_, err := c.Do(queued, "queued", func(context.Context) (cache.Result, error) { queuedCalls.Add(1); return cache.Result{}, nil })
		second <- err
	}()
	waitState(t, c, 2, 2)
	if _, err := c.Do(t.Context(), "overflow", func(context.Context) (cache.Result, error) { return cache.Result{}, nil }); !errors.Is(err, cache.ErrCapacity) {
		t.Fatal(err)
	}
	cancelQueued()
	if err := <-second; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	waitState(t, c, 0, 1)
	running, _, _ := c.State()
	if running != 1 || queuedCalls.Load() != 0 {
		t.Fatal("cancellation released actual execution", running, queuedCalls.Load())
	}
	if _, err := c.Do(t.Context(), "running", func(context.Context) (cache.Result, error) { return cache.Result{}, nil }); !errors.Is(err, cache.ErrCapacity) {
		t.Fatal("joined abandoned execution", err)
	}
	closed, cancelClose := context.WithCancel(t.Context())
	cancelClose()
	if err := c.Close(closed); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := c.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}
