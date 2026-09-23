package coord_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/coord"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

var handle = read.Handle{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 7, Combination: "C1"}

type resolver struct{}

func (resolver) Resolve(context.Context, string) (read.Handle, error) { return handle, nil }

type random struct {
	mu sync.Mutex
	n  byte
}

func (r *random) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	for i := range p {
		p[i] = r.n
	}
	return len(p), nil
}

type facts struct {
	mu         sync.Mutex
	released   map[string]bool
	executions map[string]bool
	stopped    map[string]bool
}

func (f *facts) RegisterReader(_ context.Context, _ control.SlotIdentity, id, _ string, _ time.Time) (control.Reader, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released[id] = false
	return control.Reader{ID: id}, nil
}
func (f *facts) ReleaseReader(_ context.Context, _ control.SlotIdentity, id string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released[id] = true
	return nil
}
func (f *facts) BeginSQL(_ context.Context, _ control.SlotIdentity, reader, id string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.released[reader] {
		return errors.New("released")
	}
	f.executions[id] = false
	return nil
}
func (f *facts) FinishSQL(_ context.Context, _ control.SlotIdentity, _, id string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stopped[id] {
		return errors.New("no actual termination proof")
	}
	f.executions[id] = true
	return nil
}
func (f *facts) Stopped(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped[id], nil
}
func (f *facts) counts() (active, released int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ended := range f.executions {
		if !ended {
			active++
		}
	}
	for _, closed := range f.released {
		if closed {
			released++
		}
	}
	return
}
func fixture() (*coord.Coordinator, *facts) {
	f := &facts{released: map[string]bool{}, executions: map[string]bool{}, stopped: map[string]bool{}}
	return &coord.Coordinator{Store: f, Resolver: resolver{}, Proof: f, Clock: testkit.FakeClock{Time: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)}, Random: &random{}, ProcessID: "process"}, f
}

type sourceFunc func(context.Context) ([]source.SourceRow, error)

func (f sourceFunc) ReadBatch(ctx context.Context, _ storage.Position, _, _ string, _ int) ([]source.SourceRow, error) {
	return f(ctx)
}

func TestT11_12CancellationWaitsForActualSQLAndPreservesExecutionIdentity(t *testing.T) {
	c, f := fixture()
	started := make(chan string, 1)
	release := make(chan struct{})
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	repository := read.Repository{Gate: c, Storage: sourceFunc(func(ctx context.Context) ([]source.SourceRow, error) {
		id := coord.ExecutionID(ctx)
		started <- id
		<-release
		f.mu.Lock()
		f.stopped[id] = true
		f.mu.Unlock()
		return []source.SourceRow{}, nil
	})}
	go func() {
		_, err := read.Execute(ctx, c, "", func(ctx context.Context, h read.Handle) (any, error) {
			return repository.ReadBatch(ctx, h, "synthetic", "", 1)
		})
		done <- err
	}()
	id := <-started
	if id == "" {
		t.Fatal("SQL identity not propagated")
	}
	cancel()
	if active, closed := f.counts(); active != 1 || closed != 0 {
		t.Fatal(active, closed)
	}
	select {
	case err := <-done:
		t.Fatal("returned before SQL ended", err)
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if active, closed := f.counts(); active != 0 || closed != 1 {
		t.Fatal(active, closed)
	}
}

func TestT11_13UnconfirmedSQLRemainsOccupiedAfterRequestRelease(t *testing.T) {
	c, f := fixture()
	request, err := c.Acquire(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := request.(interface {
		Context(context.Context) context.Context
	}).Context(t.Context())
	sql, err := c.BeginRead(ctx, handle)
	if err != nil {
		t.Fatal(err)
	}
	sqlCtx := sql.(interface {
		Context(context.Context) context.Context
	}).Context(ctx)
	id := coord.ExecutionID(sqlCtx)
	if err = request.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = sql.Release(t.Context()); err == nil {
		t.Fatal("missing execution proof accepted")
	}
	if active, closed := f.counts(); active != 1 || closed != 1 {
		t.Fatal(active, closed)
	}
	f.mu.Lock()
	f.stopped[id] = true
	f.mu.Unlock()
	for range 2 {
		if err = sql.Release(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err = request.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if active, closed := f.counts(); active != 0 || closed != 1 {
		t.Fatal(active, closed)
	}
	if _, err = c.BeginRead(t.Context(), handle); !errors.Is(err, read.ErrHandle) {
		t.Fatal("bare handle started SQL", err)
	}
}
