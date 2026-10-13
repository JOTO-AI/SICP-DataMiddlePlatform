package streamio

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type blockedWriter struct {
	mu       sync.Mutex
	deadline time.Time
	changed  chan struct{}
	started  chan struct{}
	once     sync.Once
}

func (w *blockedWriter) Header() http.Header { return http.Header{} }
func (w *blockedWriter) WriteHeader(int)     {}
func (w *blockedWriter) FlushError() error   { return nil }
func (w *blockedWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.deadline = at
	w.mu.Unlock()
	select {
	case w.changed <- struct{}{}:
	default:
	}
	return nil
}
func (w *blockedWriter) Write([]byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	for {
		w.mu.Lock()
		deadline := w.deadline
		w.mu.Unlock()
		if !time.Now().Before(deadline) {
			return 0, errors.New("write timeout")
		}
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-timer.C:
			return 0, errors.New("write timeout")
		case <-w.changed:
			timer.Stop()
		}
	}
}

func TestT30BlockedWriteCancellationCannotExtendDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		w := &blockedWriter{changed: make(chan struct{}, 1), started: make(chan struct{})}
		out := New(ctx, w, 5*time.Second)
		defer out.Close()
		ended := make(chan error, 1)
		go func() { _, err := out.Write([]byte("event")); ended <- err }()
		<-w.started
		start := time.Now()
		cancel()
		if out.Prepare() == nil {
			t.Fatal("cancelled write extended deadline")
		}
		if err := <-ended; err == nil {
			t.Fatal("blocked write succeeded")
		}
		if time.Since(start) != 0 {
			t.Fatal("cancellation waited for write timeout")
		}
	})
}

func TestT30HalfDisconnectedWriterHasFiniteBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &blockedWriter{changed: make(chan struct{}, 1), started: make(chan struct{})}
		out := New(t.Context(), w, 5*time.Second)
		defer out.Close()
		start := time.Now()
		if _, err := out.Write([]byte("event")); err == nil {
			t.Fatal("half disconnected write succeeded")
		}
		if time.Since(start) != 5*time.Second {
			t.Fatal("write escaped budget")
		}
	})
}

func TestT30RequestDeadlineCapsWriteBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		w := &blockedWriter{changed: make(chan struct{}, 1), started: make(chan struct{})}
		out := New(ctx, w, 5*time.Second)
		defer out.Close()
		start := time.Now()
		if _, err := out.Write([]byte("event")); err == nil {
			t.Fatal("deadline ignored")
		}
		if time.Since(start) != time.Second {
			t.Fatal("request deadline extended")
		}
	})
}
