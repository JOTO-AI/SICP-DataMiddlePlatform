package lifecycle_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/lifecycle"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT01_05StartupFailureReleasesInReverse(t *testing.T) {
	var events []string
	var factories []lifecycle.Factory
	for _, name := range []string{"one", "two", "three", "four"} {
		factories = append(factories, lifecycle.Factory{Name: name, Open: func(context.Context) (ports.Resource, error) {
			events = append(events, "open "+name)
			if name == "three" {
				return nil, errors.New("private-secret")
			}
			return testkit.CloseFunc(func(context.Context) error { events = append(events, "close "+name); return nil }), nil
		}})
	}
	m, results, err := lifecycle.Start(context.Background(), factories, time.Second)
	if err == nil || m != nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("unexpected result: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"open one", "open two", "open three", "close two", "close one"}) {
		t.Fatal(events)
	}
	if !reflect.DeepEqual(results, []lifecycle.Result{{Name: "two", State: "closed"}, {Name: "one", State: "closed"}}) {
		t.Fatal(results)
	}
}

func TestT01_06BlockedCloseHasSharedBudget(t *testing.T) {
	var m lifecycle.Manager
	block := make(chan struct{})
	exited := make(chan struct{})
	defer func() { close(block); <-exited }()
	m.Add("earlier", testkit.CloseFunc(func(context.Context) error { t.Error("must not start after exhausted budget"); return nil }))
	m.Add("blocked", testkit.CloseFunc(func(context.Context) error { defer close(exited); <-block; return nil }))
	start := time.Now()
	results := m.Close(30 * time.Millisecond)
	if time.Since(start) > time.Second {
		t.Fatal("shutdown exceeded bounded wait")
	}
	if !reflect.DeepEqual(results, []lifecycle.Result{{Name: "blocked", State: "incomplete"}, {Name: "earlier", State: "incomplete"}}) {
		t.Fatal(results)
	}
	if len(m.Close(time.Second)) != 0 {
		t.Fatal("closed twice")
	}
}

func TestStartupEdgeCases(t *testing.T) {
	t.Run("partial_resource", func(t *testing.T) {
		closed := false
		_, r, err := lifecycle.Start(context.Background(), []lifecycle.Factory{{Name: "partial", Open: func(context.Context) (ports.Resource, error) {
			return testkit.CloseFunc(func(context.Context) error { closed = true; return nil }), errors.New("failed")
		}}}, time.Second)
		if err == nil || !closed || len(r) != 1 {
			t.Fatal("partial resource leaked")
		}
	})
	t.Run("invalid_factories", func(t *testing.T) {
		opened := false
		f := lifecycle.Factory{Name: "same", Open: func(context.Context) (ports.Resource, error) { opened = true; return nil, nil }}
		if _, _, err := lifecycle.Start(context.Background(), []lifecycle.Factory{f, f}, time.Second); err == nil || opened {
			t.Fatal("did not validate before opening")
		}
	})
	t.Run("canceled_start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := lifecycle.Start(ctx, nil, time.Second); err == nil {
			t.Fatal("canceled startup accepted")
		}
	})
	t.Run("cleanup_error_continues", func(t *testing.T) {
		var m lifecycle.Manager
		m.Add("ok", testkit.CloseFunc(func(context.Context) error { return nil }))
		m.Add("bad", testkit.CloseFunc(func(context.Context) error { return errors.New("private-secret") }))
		if got := m.Close(time.Second); !reflect.DeepEqual(got, []lifecycle.Result{{Name: "bad", State: "failed"}, {Name: "ok", State: "closed"}}) {
			t.Fatal(got)
		}
	})
}
