package observability_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type dispatchClock struct{ now time.Time }
type signaledTimer struct {
	observability.SystemTimer
	waiting chan struct{}
}

func (c signaledTimer) Wait(ctx context.Context, d time.Duration) error {
	select {
	case c.waiting <- struct{}{}:
	default:
	}
	return c.SystemTimer.Wait(ctx, d)
}

type logStoreFunc func(context.Context, []observability.SafeEvent) error

func (f logStoreFunc) StoreBatch(ctx context.Context, b []observability.SafeEvent) error {
	return f(ctx, b)
}

func (c *dispatchClock) Now() time.Time { return c.now }
func (c *dispatchClock) Wait(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}
func dispatcher(t *testing.T, b *observability.Buffer, store observability.LogStore, clock *dispatchClock) *observability.Dispatcher {
	t.Helper()
	d, err := observability.NewDispatcher(b, store, clock, observability.DispatchConfig{MaxWait: 10 * time.Second, RetryWait: time.Second, Timeout: time.Second, BatchBytes: 32768})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestT05_07LowTrafficFlushesAtLongestWait(t *testing.T) {
	ctx := context.Background()
	j := newJournal()
	b := buffer(t, j)
	appendEvent(t, b, 1)
	clock := &dispatchClock{now: event().OccurredAt}
	store := &logStore{}
	d := dispatcher(t, b, store, clock)
	if delay, err := d.Pump(ctx); err != nil || delay != 10*time.Second || store.calls != 0 {
		t.Fatal("partial batch flushed early")
	}
	clock.now = clock.now.Add(9 * time.Second)
	if delay, err := d.Pump(ctx); err != nil || delay != time.Second || store.calls != 0 {
		t.Fatal("maximum wait moved")
	}
	clock.now = clock.now.Add(time.Second)
	if _, err := d.Pump(ctx); err != nil || store.calls != 1 || len(j.files) != 0 || d.Stats().Delivered != 1 {
		t.Fatal("low-volume event was not delivered")
	}
}
func TestRunWakesForFullBatchWithoutWaitingForIdleTimer(t *testing.T) {
	j := newJournal()
	b, err := observability.NewBuffer(j, sanitizer(t), 2, 32768)
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{}, 1)
	delivered := make(chan int, 1)
	d, err := observability.NewDispatcher(b, logStoreFunc(func(_ context.Context, batch []observability.SafeEvent) error { delivered <- len(batch); return nil }), signaledTimer{waiting: waiting}, observability.DispatchConfig{MaxWait: time.Minute, RetryWait: time.Second, Timeout: time.Second, BatchBytes: 32768})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not wait")
	}
	for n := 1; n <= 2; n++ {
		e := event()
		e.ID = string(rune('a' + n))
		e.OccurredAt = time.Now()
		safe, err := sanitizer(t).Sanitize(e, observability.Details{})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Append(ctx, safe); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case n := <-delivered:
		if n != 2 {
			t.Fatal("incomplete batch delivered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("append did not wake dispatcher")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher cancellation blocked")
	}
}
func TestBatchThresholdAndFailureBackoffDoNotCreateRecursiveEvents(t *testing.T) {
	ctx := context.Background()
	j := newJournal()
	b, err := observability.NewBuffer(j, sanitizer(t), 2, 32768)
	if err != nil {
		t.Fatal(err)
	}
	appendEvent(t, b, 1)
	appendEvent(t, b, 2)
	clock := &dispatchClock{now: event().OccurredAt}
	store := &logStore{down: true}
	d := dispatcher(t, b, store, clock)
	if _, err := d.Pump(ctx); err == nil || store.calls != 1 || len(j.files) != 2 {
		t.Fatal("full batch or retention failed")
	}
	if _, err := d.Pump(ctx); err != nil || store.calls != 1 {
		t.Fatal("retry bypassed backoff")
	}
	clock.now = clock.now.Add(time.Second)
	store.down = false
	if _, err := d.Pump(ctx); err != nil || store.calls != 2 || len(store.rows) != 2 || len(j.files) != 0 || d.Stats().Failures != 1 {
		t.Fatal("retry failed or delivery recursively logged itself")
	}
}

type logRows struct {
	bodies []string
	index  int
}

func (r *logRows) Next() bool {
	if r.index >= len(r.bodies) {
		return false
	}
	r.index++
	return true
}
func (r *logRows) Scan(dest ...any) error {
	if r.index < 1 || r.index > len(r.bodies) {
		return errors.New("bad row")
	}
	*(dest[0].(*string)) = r.bodies[r.index-1]
	return nil
}
func (r *logRows) Err() error { return nil }
func (r *logRows) Close()     {}
func (t *logsTx) Query(_ context.Context, sql string, args ...any) (storage.Rows, error) {
	if !strings.Contains(sql, "FROM logs.events WHERE entity_id=$1") {
		return nil, errors.New("wrong projection query")
	}
	var bodies []string
	for _, body := range t.pending {
		var envelope struct{ Event events.Event }
		if json.Unmarshal([]byte(body), &envelope) != nil {
			return nil, errors.New("bad row")
		}
		if envelope.Event.EntityID == args[0].(string) {
			bodies = append(bodies, body)
		}
	}
	return &logRows{bodies: bodies}, nil
}
func TestT05_10ProjectionRebuildIgnoresArrivalOrderAndSeparatesTransport(t *testing.T) {
	ctx := context.Background()
	s := sanitizer(t)
	started := event()
	finished := started
	finished.ID = "finished"
	finished.Phase = events.Finished
	finished.Outcome = events.Succeeded
	finished.OccurredAt = started.OccurredAt.Add(time.Second)
	transport := finished
	transport.ID = "transport"
	transport.Phase = events.Transport
	transport.Outcome = events.Interrupted
	transport.OccurredAt = started.OccurredAt.Add(2 * time.Second)
	db := &logsDB{}
	r := observability.Repository{Registry: logRegistry(t), Database: db}
	for _, e := range []events.Event{finished, transport, started} {
		safe, err := s.Sanitize(e, observability.Details{})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.StoreBatch(ctx, []observability.SafeEvent{safe}); err != nil {
			t.Fatal(err)
		}
	}
	rebuilt := observability.Repository{Registry: logRegistry(t), Database: db}
	p, err := rebuilt.ReadProjection(ctx, s, started.EntityID)
	if err != nil {
		t.Fatal(err)
	}
	if p.StartedAt == nil || !p.StartedAt.Equal(started.OccurredAt) || p.FinishedAt == nil || !p.FinishedAt.Equal(finished.OccurredAt) || p.TransportFinishedAt == nil || !p.TransportFinishedAt.Equal(transport.OccurredAt) || p.Business != events.Succeeded || p.Transport != events.Interrupted {
		t.Fatal("late start or arrival clock overwrote terminal facts")
	}
}
