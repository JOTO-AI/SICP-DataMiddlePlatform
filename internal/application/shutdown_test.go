package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/worker"
)

type blockedTask struct {
	worker.Store
	entered, canceled, release chan struct{}
	calls                      atomic.Int32
}

func (w *blockedTask) Inspect(context.Context) (update.RecoveryDecision, error) {
	return update.RecoveryDecision{Action: "inspect_local", State: control.RecoveryState{Task: &control.Task{ID: "task", Status: "running"}, Owner: control.Ownership{TaskID: "task", OwnerID: "current", Generation: 1}}}, nil
}
func (*blockedTask) Claim(context.Context, string) (control.Ownership, error) {
	return control.Ownership{}, errors.New("unexpected takeover")
}
func (w *blockedTask) Run(ctx context.Context, _ control.Task, _ control.Ownership, _ bool, _ *control.Publication) error {
	w.calls.Add(1)
	close(w.entered)
	<-ctx.Done()
	close(w.canceled)
	<-w.release
	return ctx.Err()
}
func TestT15_11TaskIgnoringCancellationRetainsIncompleteShutdown(t *testing.T) {
	w := &blockedTask{entered: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	a := &Application{done: make(chan struct{}), Engine: &worker.Engine{Store: w, Recovery: w, Work: w, Clock: testkit.FakeClock{}, Random: testkit.FixedRandom(1), Process: "current"}}
	a.Start(context.Background())
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := a.Close(ctx)
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("unbounded or falsely successful shutdown", err)
	}
	select {
	case <-w.canceled:
	default:
		t.Fatal("task was not canceled")
	}
	close(w.release)
	select {
	case <-a.done:
	case <-time.After(time.Second):
		t.Fatal("worker remained after release")
	}
	retry, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if a.Close(retry) == nil || w.calls.Load() != 1 || a.Engine.Ready(retry) {
		t.Fatal("late task return erased incomplete shutdown or restarted work")
	}
}
func TestT15_11OutstandingIndependentCacheExecutionUsesSameExitDeadline(t *testing.T) {
	flights, err := cache.NewCoalescer(1, 0, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(returned)
		_, _ = flights.Do(context.Background(), "synthetic", func(ctx context.Context) (cache.Result, error) {
			close(entered)
			<-release
			return cache.Result{}, ctx.Err()
		})
	}()
	<-entered
	a := &Application{done: make(chan struct{}), Flights: flights}
	a.Start(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if a.Close(ctx) == nil {
		t.Fatal("outstanding query reported drained")
	}
	close(release)
	<-returned
	if _, err = flights.Do(context.Background(), "new", func(context.Context) (cache.Result, error) { t.Fatal("new query admitted"); return cache.Result{}, nil }); err == nil {
		t.Fatal("closed execution queue admitted work")
	}
}

type unacknowledgedLogs struct{}

type shutdownStore func(context.Context, []observability.SafeEvent) error

func (f shutdownStore) StoreBatch(ctx context.Context, b []observability.SafeEvent) error {
	return f(ctx, b)
}

func TestT28_11BlockedLogRunDoesNotPreventOtherSourceDrain(t *testing.T) {
	key := sha256.Sum256([]byte("synthetic-dual-shutdown"))
	s, err := observability.NewSanitizer(16384, 2048, key[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	makeBuffer := func(id string) (*observability.Buffer, *observability.Directory) {
		j, err := observability.OpenDirectory(t.TempDir(), rand.Reader, 16384)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = j.Close() })
		q, err := observability.NewQuota(j, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 100, WarningPercent: 80, Policy: observability.RejectNew})
		if err != nil {
			t.Fatal(err)
		}
		b, err := observability.NewBuffer(q, s, 10, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		safe, err := s.Sanitize(events.Event{ID: id, EntityID: id, Kind: events.Request, Phase: events.Started, OccurredAt: time.Now()}, observability.Details{})
		if err != nil {
			t.Fatal(err)
		}
		if b.Append(t.Context(), safe) != nil {
			t.Fatal("append")
		}
		return b, j
	}
	bad, badFiles := makeBuffer("bad")
	good, goodFiles := makeBuffer("good")
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	badStore := shutdownStore(func(context.Context, []observability.SafeEvent) error {
		close(entered)
		defer close(returned)
		<-release
		return errors.New("unknown commit")
	})
	fast := observability.DispatchConfig{MaxWait: time.Millisecond, RetryWait: time.Second, Timeout: time.Second, BatchBytes: 1 << 20}
	badDispatch, err := observability.NewDispatcher(bad, badStore, observability.SystemTimer{}, fast)
	if err != nil {
		t.Fatal(err)
	}
	fast.MaxWait = time.Minute
	goodDispatch, err := observability.NewDispatcher(good, shutdownStore(func(context.Context, []observability.SafeEvent) error { return nil }), observability.SystemTimer{}, fast)
	if err != nil {
		t.Fatal(err)
	}
	a := &Application{done: make(chan struct{}), Logs: badDispatch, UpstreamLogs: goodDispatch}
	a.Start(context.Background())
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked source did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if a.Close(ctx) == nil {
		t.Fatal("blocked source reported clean")
	}
	close(release)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("blocked source leaked")
	}
	left, err := badFiles.Batch(t.Context(), 10, 1<<20)
	if err != nil || len(left) != 1 {
		t.Fatal("unconfirmed source lost")
	}
	left, err = goodFiles.Batch(t.Context(), 10, 1<<20)
	if err != nil || len(left) != 0 {
		t.Fatal("healthy source did not drain")
	}
}

func (unacknowledgedLogs) StoreBatch(context.Context, []observability.SafeEvent) error {
	return errors.New("commit outcome unknown")
}
func TestT15_11UnacknowledgedLogSurvivesApplicationExit(t *testing.T) {
	dir := t.TempDir()
	j, err := observability.OpenDirectory(dir, rand.Reader, 16384)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	key := sha256.Sum256([]byte("synthetic-shutdown-test-key"))
	sanitizer, err := observability.NewSanitizer(16384, 2048, key[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	quota, err := observability.NewQuota(j, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 100, WarningPercent: 80, Policy: observability.RejectNew})
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := observability.NewBuffer(quota, sanitizer, 10, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := observability.NewGuard(sanitizer, buffer)
	if err != nil {
		t.Fatal(err)
	}
	event := events.Event{ID: "task_start", EntityID: "task", Kind: events.Task, Phase: events.Started, Identity: events.Identity{TaskID: "task"}, OccurredAt: time.Date(2026, 9, 24, 1, 0, 0, 0, time.UTC)}
	if err = guard.Record(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	d, err := observability.NewDispatcher(buffer, unacknowledgedLogs{}, observability.SystemTimer{}, observability.DispatchConfig{MaxWait: time.Second, RetryWait: time.Second, Timeout: time.Second, BatchBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	a := &Application{done: make(chan struct{}), Logs: d}
	a.Start(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if a.Close(ctx) == nil {
		t.Fatal("unacknowledged delivery reported success")
	}
	batch, err := j.Batch(t.Context(), 10, 1<<20)
	if err != nil || len(batch) != 1 {
		t.Fatal("durable start event lost", err, len(batch))
	}
	if string(batch[0].Body) == "" || d.Stats().Delivered != 0 {
		t.Fatal("unknown commit was acknowledged")
	}
}
