package service_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/lifecycle"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/service"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type shutdownDB struct {
	storage.Database
	closed atomic.Int32
}

func (d *shutdownDB) Close(context.Context) error                 { d.closed.Add(1); return nil }
func (*shutdownDB) Stopped(context.Context, string) (bool, error) { return false, nil }

type shutdownRedis struct{}

func (shutdownRedis) Do(context.Context, string, ...any) (any, error) { return "PONG", nil }
func (shutdownRedis) Close(context.Context) error                     { return nil }

func TestT15_11ConfiguredServiceDoesNotRestartAnExhaustedShutdownBudget(t *testing.T) {
	for _, scenario := range []string{"blocked", "invalid-http-dependency"} {
		t.Run(scenario, func(t *testing.T) {
			v := testkit.RuntimeValues(t.TempDir())
			v["SHUTDOWN_TIMEOUT"] = "60ms"
			cfg, err := config.LoadRuntime(config.Query, func(k string) (string, bool) { s, ok := v[k]; return s, ok })
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Mkdir(cfg.LogDirectory, 0700); err != nil {
				t.Fatal(err)
			}
			db := &shutdownDB{}
			var files *assembly.FileSet
			f := assembly.Factories{Postgres: func(context.Context, config.Runtime) (assembly.Database, error) { return db, nil }, Redis: func(context.Context, config.Runtime) (assembly.Redis, error) { return shutdownRedis{}, nil }, Files: func(ctx context.Context, c config.Runtime) (*assembly.FileSet, error) {
				var e error
				files, e = assembly.RealFactories().Files(ctx, c)
				return files, e
			}}
			t.Cleanup(func() {
				if files != nil {
					_ = files.Close(context.Background())
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var shutdown service.Event
			var signalAt time.Time
			deps := service.Dependencies{Clock: testkit.FakeClock{}, Random: testkit.FixedRandom(2), Listen: func(string, string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }, Record: func(e service.Event) {
				if e.Kind == "started" {
					signalAt = time.Now()
					cancel()
				}
				if e.Kind == "shutdown" {
					shutdown = e
				}
			}}
			if scenario == "blocked" {
				deps.Factories = []lifecycle.Factory{{Name: "blocked", Open: func(context.Context) (ports.Resource, error) {
					return testkit.CloseFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }), nil
				}}}
			} else {
				deps.Random = nil
			}
			err = service.RunConfigured(ctx, cfg, f, deps)
			if err == nil {
				t.Fatal("incomplete exit reported success")
			}
			if scenario == "blocked" {
				if time.Since(signalAt) > 500*time.Millisecond || db.closed.Load() != 0 {
					t.Fatal("second cleanup budget was started", db.closed.Load())
				}
				if len(shutdown.Resources) != 4 || shutdown.Resources[3] != (lifecycle.Result{Name: "runtime", State: "incomplete"}) {
					t.Fatal(shutdown.Resources)
				}
			} else if db.closed.Load() != 1 {
				t.Fatal("untransferred runtime leaked", db.closed.Load())
			}
		})
	}
}

func TestT15_11SignalsBoundLiveHTTPAndPreserveBlockedResourceState(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			cfg, err := config.Load(config.Query, func(k string) (string, bool) {
				if k == "QUERY_API_KEY" {
					return "synthetic", true
				}
				if k == "SHUTDOWN_TIMEOUT" {
					return "60ms", true
				}
				return "", false
			})
			if err != nil {
				t.Fatal(err)
			}
			signals := make(chan os.Signal, 1)
			ctx, cancel := service.SignalContext(context.Background(), signals)
			defer cancel()
			started, entered, release, handlerDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			finished := make(chan error, 1)
			var address string
			var shutdown service.Event
			var closed atomic.Int32
			definition := contract.Definitions()[0]
			deps := service.Dependencies{Clock: testkit.FakeClock{}, Random: testkit.FixedRandom(3), QueryHandlers: map[contract.Operation]http.Handler{definition.Operation: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release; close(handlerDone) })},
				Factories: []lifecycle.Factory{{Name: "database", Open: func(context.Context) (ports.Resource, error) {
					return testkit.CloseFunc(func(context.Context) error { closed.Add(1); return nil }), nil
				}}},
				Listen: func(string, string) (net.Listener, error) {
					l, e := net.Listen("tcp", "127.0.0.1:0")
					if e == nil {
						address = l.Addr().String()
					}
					return l, e
				},
				Record: func(e service.Event) {
					if e.Kind == "started" {
						close(started)
					}
					if e.Kind == "shutdown" {
						shutdown = e
					}
				},
			}
			go func() { finished <- service.Run(ctx, cfg, deps) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("no startup")
			}
			clientDone := make(chan struct{})
			go func() {
				defer close(clientDone)
				client := http.Client{Timeout: time.Second}
				resp, _ := client.Post("http://"+address+definition.Path, "application/json", nil)
				if resp != nil {
					_ = resp.Body.Close()
				}
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("no request")
			}
			signals <- signal
			select {
			case err = <-finished:
				if err == nil {
					t.Fatal("live handler claimed drained")
				}
			case <-time.After(time.Second):
				t.Fatal("signal exceeded shutdown budget")
			}
			if closed.Load() != 0 || len(shutdown.Resources) != 2 || shutdown.Resources[0].State != "incomplete" || shutdown.Resources[1].State != "incomplete" {
				t.Fatal(shutdown.Resources, closed.Load())
			}
			close(release)
			<-handlerDone
			<-clientDone
		})
	}
}
