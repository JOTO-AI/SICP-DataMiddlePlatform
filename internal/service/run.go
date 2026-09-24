package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/application"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/lifecycle"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

type Event struct {
	Kind      string             `json:"kind"`
	At        time.Time          `json:"at"`
	Resources []lifecycle.Result `json:"resources,omitempty"`
}
type Dependencies struct {
	Clock         ports.Clock
	Random        ports.Random
	Readiness     ports.Readiness
	QueryHandlers map[contract.Operation]http.Handler
	Management    http.Handler
	Factories     []lifecycle.Factory
	Listen        func(string, string) (net.Listener, error)
	// Record must return promptly; only sanitized lifecycle facts are supplied.
	Record func(Event)
}

type serverResource struct {
	server   *http.Server
	listener net.Listener
}

func (r serverResource) Close(ctx context.Context) error {
	// A signal may arrive before Serve has registered the listener with Server.
	// Close the owned listener explicitly before releasing any dependencies.
	defer r.listener.Close()
	err := r.server.Shutdown(ctx)
	if err != nil {
		_ = r.server.Close()
	}
	return err
}

// Run owns startup and release. Canceling ctx first stops admission, then drains
// HTTP and closes dependencies in reverse order within one shutdown budget.
func Run(ctx context.Context, cfg config.Config, deps Dependencies) (runErr error) {
	if err := cfg.Validate(); err != nil {
		return err
	}
	handler, err := newHTTP(cfg.Role, cfg.APIKey, deps.Clock, deps.Random, deps.Readiness, deps.QueryHandlers, deps.Management)
	if err != nil {
		return err
	}
	record := func(kind string, results []lifecycle.Result) {
		if deps.Record != nil {
			deps.Record(Event{Kind: kind, At: deps.Clock.Now(), Resources: results})
		}
	}
	startup, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	m, results, err := lifecycle.Start(startup, deps.Factories, cfg.ShutdownTimeout)
	cancel()
	if err != nil {
		record("startup_failed", results)
		return err
	}
	defer func() {
		handler.Stop()
		results := m.Close(cfg.ShutdownTimeout)
		record("shutdown", results)
		for _, result := range results {
			if result.State != "closed" && runErr == nil {
				runErr = fmt.Errorf("shutdown incomplete; inspect resource states")
			}
		}
	}()
	if ctx.Err() != nil {
		return nil
	}
	listen := deps.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", cfg.Address)
	if err != nil {
		return fmt.Errorf("HTTP_ADDRESS: listen failed")
	}
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: cfg.ReadTimeout, ReadTimeout: cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 1 << 20,
	}
	m.Add("http", serverResource{server: server, listener: listener})
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	record("started", nil)
	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("HTTP server failed")
	}
}

// SignalContext also supports synthetic signal channels in unit tests.
func SignalContext(parent context.Context, signals <-chan os.Signal) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func Main(role config.Role) int {
	cfg, err := config.LoadRuntime(role, os.LookupEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	ctx, cancel := SignalContext(context.Background(), signals)
	defer cancel()
	err = RunConfigured(ctx, cfg, assembly.RealFactories(), Dependencies{
		Clock: ports.SystemClock{}, Random: rand.Reader,
		Record: func(event Event) { _ = json.NewEncoder(os.Stdout).Encode(event) },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// RunConfigured is the production entry boundary. Assembly failure cannot fall
// back to the dependency-free HTTP shell used by lifecycle unit tests.
func RunConfigured(ctx context.Context, cfg config.Runtime, factories assembly.Factories, deps Dependencies) (err error) {
	runtime, _, err := assembly.Open(ctx, cfg, factories)
	if err != nil {
		return err
	}
	owned := false
	defer func() {
		// Once the lifecycle manager owns runtime, its single shutdown budget
		// is authoritative even when it could not reach dependency cleanup.
		if owned {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), cfg.Service.ShutdownTimeout)
		defer cancel()
		err = errors.Join(err, runtime.Close(cleanup))
	}()
	deps.Factories = append([]lifecycle.Factory{{Name: "runtime", Open: func(context.Context) (ports.Resource, error) { owned = true; return runtime, nil }}}, deps.Factories...)
	app, err := application.New(cfg, runtime)
	if err != nil {
		return err
	}
	deps.QueryHandlers = app.Queries
	deps.Management = app.Management
	deps.Readiness = app
	deps.Factories = append(deps.Factories, lifecycle.Factory{Name: "application", Open: func(context.Context) (ports.Resource, error) { app.Start(ctx); return app, nil }})
	return Run(ctx, cfg.Service, deps)
}
