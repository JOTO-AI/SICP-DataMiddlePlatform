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
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/coord"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/handlers"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/staff"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/students"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/teaching"
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
	Maintenance  *operations.Maintenance
	Notifier     *operations.Notifier
	SourceTests  *operations.TestRunner
	Queries      map[contract.Operation]http.Handler
	Management   http.Handler
	Engine       *worker.Engine
	Flights      *cache.Coalescer
	Logs         *observability.Dispatcher
	UpstreamLogs *observability.Dispatcher
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
	if r == nil {
		return nil, errors.New("runtime missing")
	}
	if c.Databases.LogMode() == storage.DualLogs {
		if _, ok := r.LogDatabase.(storage.VerifiedLogs); !ok || r.LogSink == nil || r.UpstreamLogs == nil {
			return nil, errors.New("dual log runtime incomplete")
		}
	}
	a = &Application{runtime: r, config: c, done: make(chan struct{})}
	a.routes = runtimecontrol.Routes{Registry: c.Databases, Database: r.Database, ActiveReader: r.Active}
	clock := utcClock{}
	sink := r.LogSink
	if sink == nil {
		sink = r.Logs
	}
	guard, e := observability.NewGuard(r.Sanitizer, sink)
	if e != nil {
		return nil, e
	}
	recorder, e := events.New(ports.SystemClock{}, rand.Reader, guard, 2*time.Second)
	if e != nil {
		return nil, e
	}
	logDB := r.LogDatabase
	if logDB == nil {
		logDB = r.Database
	}
	platformSource := storage.LogSource("")
	if c.Databases.LogMode() == storage.DualLogs {
		platformSource = storage.PlatformLog
	}
	dispatchConfig := observability.DispatchConfig{MaxWait: time.Second, RetryWait: time.Second, Timeout: c.DependencyTimeout, BatchBytes: 1 << 20}
	a.Logs, e = observability.NewDispatcher(r.Logs, observability.Repository{Registry: c.Databases, Database: logDB, Source: platformSource}, observability.SystemTimer{}, dispatchConfig)
	if e != nil {
		return nil, e
	}
	if c.Databases.LogMode() == storage.DualLogs {
		a.UpstreamLogs, e = observability.NewDispatcher(r.UpstreamLogs, observability.Repository{Registry: c.Databases, Database: logDB, Source: storage.UpstreamLog}, observability.SystemTimer{}, dispatchConfig)
		if e != nil {
			return nil, e
		}
	}
	observe := func(error) { a.observations.Add(1) }
	if c.Service.Role == config.Query {
		signer, err := c.LogQuerySigner()
		if err != nil {
			return nil, err
		}
		if signer != nil {
			logs, err := logquery.NewService(logquery.Repository{Registry: c.Databases, Database: logDB, Sanitizer: r.Sanitizer}, signer, r.Sanitizer)
			if err != nil {
				return nil, err
			}
			a.Management, err = admin.LogRoutes(logs, logquery.NewStreamServer(logs), c.Service.APIKey, c.LogManagementToken())
			if err != nil {
				return nil, err
			}
		}
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
		a.Flights, e = cache.NewCoalescer(c.QueryMaxExecutions, c.QueryMaxQueue, c.QueryExecutionTimeout)
		if e != nil {
			return nil, e
		}
		pipeline := handlers.Pipeline{Grades: grades.Service{Source: reader, Times: times, Clock: clock, Limits: limits}, Timetable: timetable.Service{Source: reader, Times: times, Clock: clock, Limits: limits}, Admission: coordinator, Snapshots: snapshot.Manager{Store: r.Control, Routes: a.routes, Clock: clock, Random: rand.Reader}, Cache: cache.Executor{Results: cache.Results{Store: r.Cache, Observe: observe}, Flights: a.Flights, Validate: a.routes.Validate}, Metadata: metadata.Provider{Store: r.Control}, Times: times}
		pipeline.Organizations = organizations.Service{Source: reader, Times: times, Clock: clock, Limits: limits}
		pipeline.Students = students.Service{Source: reader, Times: times, Clock: clock, Limits: limits}
		pipeline.Staff = staff.Service{Source: reader, Times: times, Clock: clock, Limits: limits}
		pipeline.Teaching = teaching.Service{Source: reader, Times: times, Clock: clock, Limits: limits}
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
		a.Management, e = admin.Routes(auth, map[admin.Action]http.Handler{admin.Incremental: admission, admin.ForceFull: admission, admin.Rerun: admission, admin.Status: admin.StatusHandler{Store: r.Control, Selection: r.Control, Acquisition: r.Control, Work: r.Control, Diagnostics: observability.TaskDiagnostics{Repository: observability.Repository{Registry: c.Databases, Database: logDB}, Sanitizer: r.Sanitizer}}})
		if e != nil {
			return nil, e
		}
		queryProxy, e := admin.QueryProxy(c.ConsoleQueryURL, c.Service.APIKey)
		if e != nil {
			return nil, e
		}
		console := http.NewServeMux()
		opsRepo := operations.Repository{Registry: c.Databases, Database: r.Database}
		a.Maintenance = &operations.Maintenance{Repository: opsRepo, LogDatabase: logDB, Owner: r.ProcessID}
		if e = admin.MountMaintenance(console, auth, *a.Maintenance, a.OperationsStatus); e != nil {
			return nil, e
		}
		a.Notifier = &operations.Notifier{Repository: opsRepo, From: c.MailFrom, LegacyTo: c.MailTo, Owner: r.ProcessID, Audit: guard}
		if channel, ok := r.Mail.(*adapters.Mail); ok {
			a.Notifier.Channel = channel.SavedRecipients()
		} else if r.Mail != nil {
			a.Notifier.Channel = r.Mail
		}
		if e = admin.MountNotifications(console, auth, a.Notifier, operations.NewTokens(c.AuditKey)); e != nil {
			return nil, e
		}
		if sourceClient, ok := r.Source.(*adapters.Source); ok && r.SourceAdmission != nil {
			client, err := sourceClient.Client.Bounded(2 << 20)
			if err != nil {
				return nil, err
			}
			collector, err := ingest.NewCoordinator(client, ingest.BudgetConfig{RequestTimeout: c.DependencyTimeout, PageTimeout: 30 * time.Second, InitialBackoff: time.Second, MaxBackoff: time.Second, MaxAttempts: 1}, ingest.SystemTimer{}, rand.Reader, r.Control, r.SourceAdmission.LowPriority())
			if err != nil {
				return nil, err
			}
			a.SourceTests = &operations.TestRunner{Repository: opsRepo, Collector: collector, Recorder: recorder, Results: operations.NewResults(), Owner: r.ProcessID}
		}
		if e = admin.MountCatalog(console, auth, opsRepo, a.SourceTests); e != nil {
			return nil, e
		}
		views := admin.OperationsViews{Repository: operations.Repository{Registry: c.Databases, Database: r.Database}, Control: r.Control, Tokens: operations.NewTokens(c.AuditKey), Correlate: r.Sanitizer.Correlation, Active: r.Active}
		views.Traces = observability.TaskDiagnostics{Repository: observability.Repository{Registry: c.Databases, Database: logDB}, Sanitizer: r.Sanitizer}
		if e = views.Mount(console, auth, admin.StatusHandler{Store: r.Control, Selection: r.Control, Acquisition: r.Control, Work: r.Control, Diagnostics: observability.TaskDiagnostics{Repository: observability.Repository{Registry: c.Databases, Database: logDB}, Sanitizer: r.Sanitizer}}); e != nil {
			return nil, e
		}
		signer, err := c.LogQuerySigner()
		if err != nil {
			return nil, err
		}
		if signer != nil {
			logs, err := admin.LogProxy(c.ConsoleQueryURL, c.LogManagementToken(), auth)
			if err != nil {
				return nil, err
			}
			console.Handle("/admin/v1/logs/", logs)
		}
		console.Handle("/admin/", a.Management)
		console.Handle("GET /admin/v1/tasks/{$}", auth.Require(admin.TaskListHandler{Store: r.Control}))
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
	}
	return a, nil
}
func (a *Application) Start(ctx context.Context) {
	a.start.Do(func() {
		ctx, a.cancel = context.WithCancel(ctx)
		go func() {
			var group sync.WaitGroup
			if a.Logs != nil {
				go func() { _ = a.Logs.Run(ctx) }()
			}
			if a.UpstreamLogs != nil {
				go func() { _ = a.UpstreamLogs.Run(ctx) }()
			}
			if a.Engine != nil {
				group.Go(func() { a.Engine.Run(ctx, func(error) { a.observations.Add(1) }) })
			}
			if a.SourceTests != nil {
				group.Go(func() { a.SourceTests.Run(ctx) })
			}
			if a.Notifier != nil {
				group.Go(func() { a.Notifier.Run(ctx) })
			}
			if a.Maintenance != nil {
				group.Go(func() { a.Maintenance.Run(ctx) })
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
	// Drain both sources concurrently under the same shutdown budget. A stalled
	// target must not consume the healthy target's chance to acknowledge files.
	results := make(chan error, 2)
	count := 0
	for _, d := range []*observability.Dispatcher{a.Logs, a.UpstreamLogs} {
		if d != nil {
			count++
			go func() { results <- d.Close(ctx) }()
		}
	}
	for i := 0; i < count; i++ {
		err = errors.Join(err, <-results)
	}
	return err
}
func (a *Application) Status(ctx context.Context) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	type logHealth struct {
		source storage.LogSource
		ready  bool
	}
	logResults := make(chan logHealth, 2)
	_, dual := a.runtime.LogDatabase.(storage.VerifiedLogs)
	if gate, ok := a.runtime.LogDatabase.(storage.VerifiedLogs); ok {
		for _, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
			go func() { logResults <- logHealth{source, gate.Ready(ctx, source)} }()
		}
	}
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
	status := map[string]bool{"dependencies_ready": dependencies, "query_ready": query, "sync_ready": sync, "log_single_legacy": a.config.Databases.LogMode() == storage.SingleLegacy}
	if dual {
		status["platform_log_ready"], status["upstream_log_ready"] = false, false
		for i := 0; i < 2; i++ {
			select {
			case h := <-logResults:
				status[string(h.source)+"_log_ready"] = h.ready
			case <-ctx.Done():
				return status
			}
		}
	}
	return status
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
