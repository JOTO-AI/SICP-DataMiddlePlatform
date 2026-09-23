package cache_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func TestT11_08CacheOutageCanLoadOnlyConfirmedPinnedPosition(t *testing.T) {
	h := read.Handle{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 9, Combination: "C1"}
	for _, confirmed := range []bool{true, false} {
		r := &redisMemory{getErr: errors.New("cache offline"), ack: "OK"}
		c := group(t, 1, 1)
		var observed, loads atomic.Int32
		e := cache.Executor{Results: cache.Results{Store: cache.Redis{Client: r}, Observe: func(error) { observed.Add(1) }}, Flights: c, Validate: func(_ context.Context, got read.Handle) error {
			if got != h || !confirmed {
				return read.ErrHandle
			}
			return nil
		}}
		got, err := e.Query(t.Context(), h, contract.StudentGrades, contract.StudentGradesRequest{StudentNumber: "001"}, func(_ context.Context, pinned read.Handle) ([]byte, error) {
			loads.Add(1)
			if pinned != h {
				return nil, errors.New("route changed")
			}
			return []byte(`{"total":0,"items":[]}`), nil
		})
		if confirmed {
			if err != nil || got.Hit || string(got.Data) != `{"total":0,"items":[]}` || loads.Load() != 1 || observed.Load() == 0 {
				t.Fatal(got, err, loads.Load())
			}
		} else {
			if !errors.Is(err, read.ErrHandle) || loads.Load() != 0 || len(r.calls) != 0 {
				t.Fatal("unconfirmed route accessed cache/SQL", err, r.calls)
			}
		}
		if err := c.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestT11_08ClosedPositionBeforeExecutionCannotDegrade(t *testing.T) {
	h := read.Handle{Position: storage.Position{Instance: storage.Main}, Generation: 1, Combination: "C1"}
	r := &redisMemory{getErr: errors.New("cache offline")}
	c := group(t, 1, 0)
	var checks atomic.Int32
	e := cache.Executor{Results: cache.Results{Store: cache.Redis{Client: r}, Observe: func(error) {}}, Flights: c, Validate: func(context.Context, read.Handle) error {
		if checks.Add(1) > 1 {
			return read.ErrHandle
		}
		return nil
	}}
	_, err := e.Query(t.Context(), h, contract.StudentGrades, contract.StudentGradesRequest{StudentNumber: "001"}, func(context.Context, read.Handle) ([]byte, error) { t.Error("SQL on closed route"); return nil, nil })
	if !errors.Is(err, read.ErrHandle) {
		t.Fatal(err)
	}
	if err = c.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}
