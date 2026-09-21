package service_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/lifecycle"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/service"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

var fakeTime = time.Date(2026, 9, 18, 9, 0, 0, 0, time.FixedZone("test", 8*3600))

func TestT01_07LiveButNotBusinessReady(t *testing.T) {
	for _, role := range []config.Role{config.Query, config.Worker} {
		h, err := service.NewHTTP(role, testkit.FakeClock{Time: fakeTime}, testkit.FixedRandom(1), nil)
		if err != nil {
			t.Fatal(err)
		}
		for path, status := range map[string]int{"/livez": 200, "/readyz": 503} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
			if rec.Code != status {
				t.Fatalf("%s %s: %d", role, path, rec.Code)
			}
		}
		for _, d := range contract.Definitions() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", d.Path, nil))
			want := 503
			if role == config.Worker {
				want = 404
			}
			if rec.Code != want {
				t.Fatal(role, d.Operation, rec.Code)
			}
		}
	}
}

func TestT01_08InjectedClockAndRandomReachRequestContext(t *testing.T) {
	var observed service.Metadata
	h, err := service.NewHTTP(config.Query, testkit.FakeClock{Time: fakeTime}, testkit.FixedRandom(0xab), testkit.ReadyFunc(func(ctx context.Context) bool { observed = service.RequestMetadata(ctx); return false }))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/readyz", nil)
	r.Header.Set("X-Request-ID", "untrusted-client-id")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if observed.RequestID != strings.Repeat("ab", 16) || !observed.StartedAt.Equal(fakeTime) || rec.Header().Get("X-Request-ID") != observed.RequestID {
		t.Fatal(observed)
	}
}

func TestT01_10SignalStopsAdmissionAndReleasesResources(t *testing.T) {
	for _, role := range []config.Role{config.Query, config.Worker} {
		t.Run(string(role), func(t *testing.T) {
			cfg, err := config.Load(role, func(key string) (string, bool) {
				if key == "QUERY_API_KEY" {
					return "synthetic-test-key", true
				}
				return "", false
			})
			if err != nil {
				t.Fatal(err)
			}
			signals := make(chan os.Signal, 1)
			ctx, cancel := service.SignalContext(context.Background(), signals)
			defer cancel()
			started := make(chan struct{})
			finished := make(chan error, 1)
			var events []service.Event
			var released []string
			var address string
			var factories []lifecycle.Factory
			for _, name := range []string{"one", "two"} {
				factories = append(factories, lifecycle.Factory{Name: name, Open: func(context.Context) (ports.Resource, error) {
					return testkit.CloseFunc(func(context.Context) error {
						conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
						if err == nil {
							conn.Close()
							t.Error("HTTP listener still accepts during dependency release")
						}
						released = append(released, name)
						return nil
					}), nil
				}})
			}
			deps := service.Dependencies{Clock: testkit.FakeClock{Time: fakeTime}, Random: testkit.FixedRandom(1), Factories: factories,
				Listen: func(string, string) (net.Listener, error) {
					l, e := net.Listen("tcp", "127.0.0.1:0")
					if e == nil {
						address = l.Addr().String()
					}
					return l, e
				},
				Record: func(e service.Event) {
					events = append(events, e)
					if e.Kind == "started" {
						close(started)
					}
				},
			}
			go func() { finished <- service.Run(ctx, cfg, deps) }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("startup did not finish")
			}
			signals <- os.Interrupt
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("signal not handled")
			}
			if !reflect.DeepEqual(released, []string{"two", "one"}) {
				t.Fatal(released)
			}
			if len(events) != 2 || events[0].Kind != "started" || events[1].Kind != "shutdown" {
				t.Fatal(events)
			}
			for _, e := range events {
				if !e.At.Equal(fakeTime) {
					t.Fatal("global clock used")
				}
			}
			want := []lifecycle.Result{{Name: "http", State: "closed"}, {Name: "two", State: "closed"}, {Name: "one", State: "closed"}}
			if !reflect.DeepEqual(events[1].Resources, want) {
				t.Fatal(events[1].Resources)
			}
		})
	}
}

func TestListenFailureCleansInitializedDependencies(t *testing.T) {
	cfg, err := config.Load(config.Worker, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	var event service.Event
	err = service.Run(context.Background(), cfg, service.Dependencies{
		Clock: testkit.FakeClock{Time: fakeTime}, Random: testkit.FixedRandom(1),
		Factories: []lifecycle.Factory{{Name: "resource", Open: func(context.Context) (ports.Resource, error) {
			return testkit.CloseFunc(func(context.Context) error { closed = true; return nil }), nil
		}}},
		Listen: func(string, string) (net.Listener, error) { return nil, errors.New("private-secret") }, Record: func(e service.Event) { event = e },
	})
	if err == nil || strings.Contains(err.Error(), "private-secret") || !closed || len(event.Resources) != 1 {
		t.Fatal("listen failure was not safely cleaned", err)
	}
}

func TestDrainingOverridesReadinessAndBlocksQueries(t *testing.T) {
	h, err := service.NewHTTP(config.Query, testkit.FakeClock{Time: fakeTime}, testkit.FixedRandom(1), testkit.ReadyFunc(func(context.Context) bool { return true }))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 503 {
		t.Fatal("dependencies cannot make unresolved handlers ready", rec.Code)
	}
	h.Stop()
	for _, path := range []string{"/readyz", string(contract.Definitions()[0].Path)} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 503 {
			t.Fatal(path, rec.Code)
		}
	}
}

func TestShutdownTimeoutIsRecordedAndFailsRun(t *testing.T) {
	cfg, err := config.Load(config.Worker, func(key string) (string, bool) {
		if key == "SHUTDOWN_TIMEOUT" {
			return "30ms", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var shutdown service.Event
	err = service.Run(ctx, cfg, service.Dependencies{
		Clock: testkit.FakeClock{Time: fakeTime}, Random: testkit.FixedRandom(1),
		Factories: []lifecycle.Factory{{Name: "blocked", Open: func(context.Context) (ports.Resource, error) {
			return testkit.CloseFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }), nil
		}}},
		Listen: func(string, string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") },
		Record: func(e service.Event) {
			if e.Kind == "started" {
				cancel()
			}
			if e.Kind == "shutdown" {
				shutdown = e
			}
		},
	})
	if err == nil {
		t.Fatal("incomplete shutdown reported success")
	}
	if len(shutdown.Resources) != 2 || shutdown.Resources[1].State != "incomplete" {
		t.Fatal(shutdown)
	}
}

type failingRandom struct{}

func (failingRandom) Read([]byte) (int, error) { return 0, errors.New("private-secret") }
func TestRandomFailureIsSanitized(t *testing.T) {
	h, err := service.NewHTTP(config.Query, testkit.FakeClock{Time: fakeTime}, failingRandom{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/livez", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "private-secret") || rec.Header().Get("X-Request-ID") != "" {
		t.Fatal(rec.Body.String())
	}
}

var _ http.Handler = (*service.HTTP)(nil)
