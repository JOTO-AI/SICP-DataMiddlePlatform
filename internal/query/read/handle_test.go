package read_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var original = read.Handle{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 7, Combination: "C7"}

type lease struct {
	h                read.Handle
	closed, released int
	err              error
}

func (l *lease) Handle() read.Handle { return l.h }
func (l *lease) Close(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	l.closed++
	return l.err
}
func (l *lease) Release(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	l.released++
	return l.err
}

type admissionFunc func(context.Context, string) (read.RequestLease, error)

func (f admissionFunc) Acquire(c context.Context, s string) (read.RequestLease, error) {
	return f(c, s)
}

type gateFunc func(context.Context, read.Handle) (read.SQLLease, error)

func (f gateFunc) BeginRead(c context.Context, h read.Handle) (read.SQLLease, error) { return f(c, h) }

type batchFunc func(context.Context, storage.Position, string, string, int) ([]source.SourceRow, error)

func (f batchFunc) ReadBatch(c context.Context, p storage.Position, t, a string, n int) ([]source.SourceRow, error) {
	return f(c, p, t, a, n)
}

func TestT06_06AllReadsStayPinnedAcrossRouteChange(t *testing.T) {
	active := original
	request := &lease{h: original}
	acquires := 0
	var positions []storage.Position
	var handles []read.Handle
	sqlLeases := []*lease{}
	repo := read.Repository{Gate: gateFunc(func(_ context.Context, h read.Handle) (read.SQLLease, error) {
		handles = append(handles, h)
		l := &lease{h: h}
		sqlLeases = append(sqlLeases, l)
		return l, nil
	}),
		Storage: batchFunc(func(_ context.Context, p storage.Position, _, _ string, _ int) ([]source.SourceRow, error) {
			positions = append(positions, p)
			active = read.Handle{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 9, Combination: "C9"}
			return []source.SourceRow{}, nil
		})}
	got, err := read.Execute(context.Background(), admissionFunc(func(_ context.Context, s string) (read.RequestLease, error) {
		acquires++
		if s != "snapshot" {
			t.Fatal(s)
		}
		request.h = active
		return request, nil
	}), "snapshot", func(ctx context.Context, h read.Handle) (any, error) {
		for _, table := range []string{"grades", "students", "courses"} {
			if _, err := repo.ReadBatch(ctx, h, table, "", 20); err != nil {
				return nil, err
			}
		}
		return "complete", nil
	})
	if err != nil || got != "complete" || acquires != 1 || request.closed != 1 {
		t.Fatal(got, err, acquires, request.closed)
	}
	if !reflect.DeepEqual(handles, []read.Handle{original, original, original}) || !reflect.DeepEqual(positions, []storage.Position{original.Position, original.Position, original.Position}) {
		t.Fatal(handles, positions)
	}
	for _, l := range sqlLeases {
		if l.released != 1 {
			t.Fatal("SQL registration leaked")
		}
	}
}

func TestT06_07InvalidAndStaleHandlesNeverReadDefault(t *testing.T) {
	for name, h := range map[string]read.Handle{
		"zero": {}, "instance": {Position: storage.Position{Instance: "X"}, Generation: 1, Combination: "C"},
		"slot":       {Position: storage.Position{Instance: storage.Main, Slot: 2}, Generation: 1, Combination: "C"},
		"generation": {Position: original.Position, Combination: "C"}, "combination": {Position: original.Position, Generation: 1},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			r := read.Repository{Gate: gateFunc(func(context.Context, read.Handle) (read.SQLLease, error) { calls++; return nil, nil }), Storage: batchFunc(func(context.Context, storage.Position, string, string, int) ([]source.SourceRow, error) {
				t.Fatal("SQL reached")
				return nil, nil
			})}
			if rows, err := r.ReadBatch(context.Background(), h, "grades", "", 1); err == nil || rows != nil || calls != 0 {
				t.Fatal(rows, err, calls)
			}
		})
	}
	for _, kind := range []string{"generation", "combination", "position", "rejected", "nil"} {
		t.Run(kind, func(t *testing.T) {
			l := &lease{h: original}
			calls := 0
			switch kind {
			case "generation":
				l.h.Generation++
			case "combination":
				l.h.Combination = "C8"
			case "position":
				l.h.Position.Slot = 1
			}
			r := read.Repository{Gate: gateFunc(func(context.Context, read.Handle) (read.SQLLease, error) {
				if kind == "rejected" {
					return nil, read.ErrHandle
				}
				if kind == "nil" {
					return nil, nil
				}
				return l, nil
			}), Storage: batchFunc(func(context.Context, storage.Position, string, string, int) ([]source.SourceRow, error) {
				calls++
				return nil, nil
			})}
			if rows, err := r.ReadBatch(context.Background(), original, "grades", "", 1); err == nil || rows != nil || calls != 0 {
				t.Fatal(rows, err, calls)
			}
			if kind != "rejected" && kind != "nil" && l.released != 1 {
				t.Fatal("mismatched lease leaked")
			}
		})
	}
}

func TestT06_07CancellationCannotReleaseRunningSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	l := &lease{h: original}
	released := make(chan struct{})
	repo := read.Repository{Gate: gateFunc(func(context.Context, read.Handle) (read.SQLLease, error) {
		return &signalLease{lease: l, released: released}, nil
	}), Storage: batchFunc(func(context.Context, storage.Position, string, string, int) ([]source.SourceRow, error) {
		close(started)
		<-finish
		return []source.SourceRow{{}}, nil
	})}
	go func() {
		rows, err := repo.ReadBatch(ctx, original, "grades", "", 1)
		if rows != nil {
			done <- errors.New("partial result")
			return
		}
		done <- err
	}()
	<-started
	cancel()
	select {
	case <-released:
		t.Error("released while SQL still running")
	default:
	}
	close(finish)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if l.released != 1 {
		t.Fatal("not released after SQL completion")
	}
}

type signalLease struct {
	*lease
	released chan struct{}
}

func (l *signalLease) Release(c context.Context) error { close(l.released); return l.lease.Release(c) }

func TestT06_07FailedReadOrLeaseCleanupDropsResult(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		l := &lease{h: original}
		failure := errors.New("synthetic failure")
		if cleanup {
			l.err = failure
		}
		r := read.Repository{Gate: gateFunc(func(context.Context, read.Handle) (read.SQLLease, error) { return l, nil }), Storage: batchFunc(func(context.Context, storage.Position, string, string, int) ([]source.SourceRow, error) {
			if cleanup {
				return []source.SourceRow{{}}, nil
			}
			return []source.SourceRow{{}}, failure
		})}
		if rows, err := r.ReadBatch(context.Background(), original, "grades", "", 1); !errors.Is(err, failure) || rows != nil || l.released != 1 {
			t.Fatal(rows, err, l.released)
		}
	}
	l := &lease{h: original, err: errors.New("close failure")}
	v, err := read.Execute(context.Background(), admissionFunc(func(context.Context, string) (read.RequestLease, error) { return l, nil }), "", func(context.Context, read.Handle) (any, error) { return "partial", nil })
	if err == nil || v != nil || l.closed != 1 {
		t.Fatal(v, err, l.closed)
	}
}

func TestT06_07InvalidRequestAdmissionAndCanceledCallback(t *testing.T) {
	for _, kind := range []string{"invalid", "nil", "error", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			l := &lease{h: original}
			calls := 0
			if kind == "invalid" {
				l.h.Generation = 0
			}
			v, err := read.Execute(ctx, admissionFunc(func(context.Context, string) (read.RequestLease, error) {
				if kind == "nil" {
					return nil, nil
				}
				if kind == "error" {
					return nil, read.ErrHandle
				}
				return l, nil
			}), "", func(context.Context, read.Handle) (any, error) { calls++; cancel(); return "partial", nil })
			if err == nil || v != nil {
				t.Fatal(v, err)
			}
			if kind == "canceled" {
				if calls != 1 || l.closed != 1 {
					t.Fatal(calls, l.closed)
				}
			} else if calls != 0 {
				t.Fatal("invalid admission ran callback")
			}
			if kind == "invalid" && l.closed != 1 {
				t.Fatal("invalid lease leaked")
			}
		})
	}
}
