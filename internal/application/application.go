// Package application assembles the business pipelines above real transports.
package application

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/coord"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/handlers"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/runtimecontrol"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/worker"
)

type utcClock struct{}

func (utcClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type Application struct {
	Queries      map[contract.Operation]http.Handler
	Management   http.Handler
	Engine       *worker.Engine
	Flights      *cache.Coalescer
	Logs         *observability.Dispatcher
	Sender       *notification.Sender
	runtime      *assembly.Runtime
	config       config.Runtime
	routes       runtimecontrol.Routes
	observations atomic.Uint64
	cancel       context.CancelFunc
	done         chan struct{}
	start        sync.Once
	closeOnce    sync.Once
	closed       chan struct{}
	closeErr     error
}

func New(c config.Runtime, r *assembly.Runtime) (a *Application, err error) {
	a = &Application{runtime: r, config: c, done: make(chan struct{})}
	a.routes = runtimecontrol.Routes{Registry: c.Databases, Database: r.Database, ActiveReader: r.Active}
	clock := utcClock{}
	guard, e := observability.NewGuard(r.Sanitizer, r.Logs)
	if e != nil {
		return nil, e
	}
	recorder, e := events.New(clock, rand.Reader, guard, 2*time.Second)
	if e != nil {
		return nil, e
	}
	a.Logs, e = observability.NewDispatcher(r.Logs, observability.Repository{Registry: c.Databases, Database: r.Database}, observability.SystemTimer{}, observability.DispatchConfig{MaxWait: time.Second, RetryWait: time.Second, Timeout: c.DependencyTimeout, BatchBytes: 1 << 20})
	if e != nil {
		return nil, e
	}
	observe := func(error) { a.observations.Add(1) }
	if c.Service.Role == config.Query {
		proof, ok := r.Database.(coord.ExecutionProof)
		if !ok {
			return nil, errors.New("database execution proof unavailable")
		}
		coordinator := &coord.Coordinator{Store: r.Control, Resolver: a.routes, Proof: proof, Clock: clock, Random: rand.Reader, ProcessID: r.ProcessID}
		reader := read.Repository{Storage: source.Repository{Registry: c.Databases, Database: r.Database}, Gate: coordinator}
		times, e := compute.NewTimes(c.Service.Timezone)
		if e != nil {
			return nil, e
		}
		limits := read.DefaultLimits()
		a.Flights, e = cache.NewCoalescer(8, 64, 30*time.Second)
		if e != nil {
			return nil, e
		}
		pipeline := handlers.Pipeline{Grades: grades.Service{Source: reader, Times: times, Clock: clock, Limits: limits}, Timetable: timetable.Service{Source: reader, Times: times, Clock: clock, Limits: limits}, Admission: coordinator, Snapshots: snapshot.Manager{Store: r.Control, Routes: a.routes, Clock: clock, Random: rand.Reader}, Cache: cache.Executor{Results: cache.Results{Store: r.Cache, Observe: observe}, Flights: a.Flights, Validate: a.routes.Validate}, Metadata: metadata.Provider{Store: r.Control}, Times: times}
		a.Queries, e = pipeline.Handlers(c.Service.APIKey, recorder, func(_ *events.Span, e error) { observe(e) })
		if e != nil {
			return nil, e
		}
	} else {
		auth, e := admin.NewSessions(c.AdminUsername, c.AdminPassword, c.Service.APIKey, c.ConsoleCookieSecure)
		if e != nil {
			return nil, e
		}
		admission := admin.Admission{Store: r.Control, Clock: clock, Random: rand.Reader}
		a.Management, e = admin.Routes(auth, map[admin.Action]http.Handler{admin.Initialize: admission, admin.Sync: admission, admin.ForceFull: admission, admin.Rerun: admission, admin.Status: admin.StatusHandler{Store: r.Control}})
		if e != nil {
			return nil, e
		}
		queryProxy, e := admin.QueryProxy(c.ConsoleQueryURL, c.Service.APIKey)
		if e != nil {
			return nil, e
		}
		console := http.NewServeMux()
		console.Handle("/admin/", a.Management)
		console.HandleFunc("POST /console-api/login", auth.Login)
		console.HandleFunc("GET /console-api/session", auth.Current)
		console.HandleFunc("POST /console-api/logout", auth.Logout)
		console.Handle("/console-api/query/", auth.Require(queryProxy))
		console.Handle("/console-api/worker/", auth.Require(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			switch {
			case req.Method == "GET" && req.URL.Path == "/console-api/worker/livez":
				_ = json.NewEncoder(w).Encode(map[string]bool{"live": true})
			case req.Method == "GET" && req.URL.Path == "/console-api/worker/readyz":
				status := a.Status(req.Context())
				status["ready"] = status["sync_ready"]
				if !status["ready"] {
					w.WriteHeader(503)
				}
				_ = json.NewEncoder(w).Encode(status)
			default:
				http.NotFound(w, req)
			}
		})))
		a.Management = console
		termination := worker.Termination{Runtime: r}
		a.Engine = &worker.Engine{Store: r.Control, Recovery: update.Recovery{Store: r.Control, Active: r.Active, Execution: termination, Restorer: worker.Restorer{Runtime: r}}, Work: worker.Executor{Runtime: r, Config: c, Clock: clock, Recorder: recorder, Sink: guard, Observe: observe}, Clock: clock, Random: rand.Reader, Process: r.ProcessID}
		if c.MailEnabled() {
			a.Sender, e = notification.NewSender(r.Control, r.Mail, clock, r.Sanitizer, notification.DeliveryConfig{From: c.MailFrom, To: c.MailTo, MaxAttempts: 8, Batch: 20, RetryDelay: time.Minute, SendTimeout: c.DependencyTimeout, StoreTimeout: c.DependencyTimeout, Lease: 3*c.DependencyTimeout + time.Second})
			if e != nil {
				return nil, e
			}
		}
	}
	return a, nil
}
func (a *Application) Start(ctx context.Context) {
	a.start.Do(func() {
		ctx, a.cancel = context.WithCancel(ctx)
		go func() {
			var group sync.WaitGroup
			if a.Logs != nil {
				group.Go(func() { _ = a.Logs.Run(ctx) })
			}
			if a.Engine != nil {
				group.Go(func() { a.Engine.Run(ctx, func(error) { a.observations.Add(1) }) })
			}
			if a.Sender != nil {
				group.Go(func() {
					for {
						_, e := a.Sender.Pump(ctx)
						if e != nil && ctx.Err() == nil {
							a.observations.Add(1)
						}
						timer := time.NewTimer(time.Second)
						select {
						case <-ctx.Done():
							timer.Stop()
							return
						case <-timer.C:
						}
					}
				})
			}
			group.Wait()
			close(a.done)
		}()
	})
}
func (a *Application) Close(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("shutdown deadline required")
	}
	a.closeOnce.Do(func() {
		a.closed = make(chan struct{})
		go func() { a.closeErr = a.drain(ctx); close(a.closed) }()
	})
	select {
	case <-ctx.Done():
		return errors.New("application shutdown incomplete")
	case <-a.closed:
		return a.closeErr
	}
}
func (a *Application) drain(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
		select {
		case <-a.done:
		case <-ctx.Done():
			return errors.New("application work remains recoverable")
		}
	}
	var err error
	if a.Flights != nil {
		err = errors.Join(err, a.Flights.Close(ctx))
	}
	if a.Logs != nil {
		err = errors.Join(err, a.Logs.Close(ctx))
	}
	return err
}
func (a *Application) Status(ctx context.Context) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	dependencies := false
	if pong, e := a.runtime.Redis.Do(ctx, "PING"); e == nil && pong == "PONG" {
		tx, e := a.runtime.Database.Begin(ctx, a.config.Databases.Control())
		if e == nil {
			var one int
			e = tx.QueryRow(ctx, "SELECT 1").Scan(&one)
			if e == nil {
				e = tx.Commit(ctx)
			}
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			_ = tx.Rollback(cleanup)
			stop()
			dependencies = e == nil && one == 1
		}
	}
	query := a.queryReady(ctx)
	sync := dependencies && a.Engine != nil && a.Engine.Ready(ctx)
	return map[string]bool{"dependencies_ready": dependencies, "query_ready": query, "sync_ready": sync}
}
func (a *Application) queryReady(ctx context.Context) bool {
	h, err := a.routes.Active(ctx)
	if err != nil {
		return false
	}
	c, err := a.config.Databases.Business(h.Position)
	if err != nil {
		return false
	}
	var db storage.Database = a.runtime.Database
	if a.config.Service.Role == config.Worker {
		db = adapters.QueryDatabase{Database: db}
	}
	tx, err := db.Begin(ctx, c)
	if err != nil {
		return false
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	for _, table := range source.Catalog() {
		var allowed bool
		if err = tx.QueryRow(ctx, "SELECT has_table_privilege(current_user,$1,'SELECT')", "source."+table.Name).Scan(&allowed); err != nil || !allowed {
			return false
		}
	}
	return tx.Commit(ctx) == nil
}
func (a *Application) Ready(ctx context.Context) bool {
	v := a.Status(ctx)
	if a.config.Service.Role == config.Query {
		return v["query_ready"]
	}
	return v["sync_ready"]
}
