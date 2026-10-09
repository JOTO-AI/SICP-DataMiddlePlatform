package worker

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/coord"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/runtimecontrol"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type Executor struct {
	Runtime  *assembly.Runtime
	Config   config.Runtime
	Clock    ports.Clock
	Recorder *events.Recorder
	Sink     events.Sink
	Observe  func(error)
}
type retained struct {
	store    *staging.Store
	manifest candidate.Manifest
	task     string
}

func (r retained) Full(ctx context.Context) (lease *staging.Lease, err error) {
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "sealing", Combination: r.manifest.Combination}, "")
	defer func() { end(err) }()
	l, e := r.store.Retain(ctx, r.manifest.Input, r.task)
	if e != nil {
		return nil, errors.Join(update.ErrReacquire, e)
	}
	return l, nil
}
func (w Executor) taskEvent(ctx context.Context, t control.Task, end bool) error {
	if t.StartedAt == nil {
		return control.ErrEntity
	}
	e := events.Event{ID: t.ID + "_start", EntityID: t.ID, Kind: events.Task, Phase: events.Started, Identity: events.Identity{TaskID: t.ID}, OccurredAt: *t.StartedAt}
	if end {
		if t.FinishedAt == nil {
			return nil
		}
		e.ID = t.ID + "_end"
		e.Phase = events.Finished
		e.OccurredAt = *t.FinishedAt
		e.Outcome = events.Succeeded
		if t.Status == "failed" {
			e.Outcome = events.Failed
		}
	}
	return w.Sink.Record(ctx, e)
}
func (w Executor) Run(ctx context.Context, task control.Task, owner control.Ownership, resume bool, pending *control.Publication) (err error) {
	r := w.Runtime
	repo := r.Control.WithOwnership(owner)
	// Control times are authoritative. A recovered event uses the original
	// start time and stable event ID; it never synthesizes an interruption end.
	if err = w.taskEvent(ctx, task, false); err != nil {
		return err
	}
	ctx = events.WithRecorder(ctx, w.Recorder, events.Identity{TaskID: task.ID})
	ctx = events.WithBuildTrace(ctx, owner.Generation, w.Observe)
	ctx = events.WithBuildStart(ctx, w.Clock.Now, repo.StartBuildTiming, w.Observe)
	if task.Status == "failed" || task.Status == "success_published" || task.Status == "success_unchanged" {
		return w.finish(ctx, task, owner)
	}
	ctx, stopActivity := activity.Track(ctx, w.Clock.Now, repo.SaveWorkProgress, w.Observe)
	defer stopActivity()
	finish := func(finishCtx context.Context) error { stopActivity(); return w.finish(finishCtx, task, owner) }
	activity.Report(ctx, "preparing", "", "")
	if resume || pending != nil {
		activity.Report(ctx, "recovering", "", "")
	}
	reconciler := publication.Reconciler{Store: repo, Active: r.Active, Atomic: r.Active, Clock: w.Clock}
	finisher := update.Finisher{Store: repo, Delivery: reconciler, Clock: w.Clock}
	if pending != nil {
		event, e := finisher.Recover(ctx, *pending)
		if e != nil {
			return e
		}
		if event.Kind == "bundle_publish" {
			return finish(ctx)
		}
	}
	if task.Status == "published_pending_progress" {
		activity.Report(ctx, "confirming_progress", "", "")
		event, e := repo.RecoveryPublication(ctx, task.ID+"-publish")
		if e != nil {
			return e
		}
		if event == nil {
			return update.ErrRecoveryPending
		}
		if _, e = finisher.Recover(ctx, *event); e != nil {
			return e
		}
		return finish(ctx)
	}
	err = w.execute(ctx, task, owner, resume, reconciler)
	if err == nil {
		return finish(ctx)
	}
	// Only a positively failed acquisition or verified unusable seal terminates
	// the task here. Unknown DB/publication/build outcomes stay recoverable.
	var acquisition *ingest.AcquisitionError
	if errors.As(err, &acquisition) || errors.Is(err, update.ErrReacquire) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var at *time.Time
		reason := "corrupt_seal"
		if !resume && ctx.Err() == nil {
			now := w.Clock.Now()
			at = &now
		}
		if errors.As(err, &acquisition) {
			reason = "acquisition_failed"
		}
		if e := repo.FailTask(cleanup, reason, at); e != nil {
			return errors.Join(err, e)
		}
		return errors.Join(err, finish(cleanup))
	}
	return err
}
func (w Executor) finish(ctx context.Context, t control.Task, owner control.Ownership) error {
	view, e := w.Runtime.Control.TaskStatus(ctx, t.ID)
	if e != nil {
		return e
	}
	if view.Task.Status != "failed" && view.Task.Status != "success_published" && view.Task.Status != "success_unchanged" {
		return update.ErrRecoveryPending
	}
	if e = w.taskEvent(ctx, view.Task, true); e != nil {
		return e
	}
	if view.Task.Status == "success_published" {
		if e := w.recordUsability(ctx, view.Task, owner); e != nil && w.Observe != nil {
			w.Observe(e)
		}
	}
	if view.Task.Status != "failed" {
		if e := w.maintain(ctx, view.Task, owner); e != nil {
			// Discovery failures also receive an independent bounded record.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			recordErr := w.Runtime.Control.WithOwnership(owner).RecordMaintenance(cleanup, "scan", false, w.Clock.Now())
			cancel()
			if w.Observe != nil {
				w.Observe(errors.Join(e, recordErr))
			}
		}
	}
	return w.Runtime.Control.Release(ctx, owner)
}
func (w Executor) execute(ctx context.Context, task control.Task, owner control.Ownership, resume bool, delivery publication.Delivery) error {
	r := w.Runtime
	c := w.Config
	repo := r.Control.WithOwnership(owner)
	store := runtimecontrol.Store{Repository: r.Control, Owner: owner, Clock: w.Clock}
	files, e := staging.NewStore(task.ID, r.Files.Staging, 64<<20)
	if e != nil {
		return e
	}
	targets, options := targetPreparation(c, r.Files.Targets, "C"+task.ID)
	writer := source.Repository{Registry: c.Databases, Database: r.Database, Gate: store, UseCopy: c.BuildImportMode == "copy"}
	reader := source.Repository{Registry: c.Databases, Database: adapters.QueryDatabase{Database: r.Database}}
	candidateReads := read.Repository{Storage: reader, Gate: store}
	times, e := compute.NewTimes(c.Service.Timezone)
	if e != nil {
		return e
	}
	limits := read.BackgroundLimits(c.BuildCheckTimeout)
	online := func(reader read.Matcher) candidate.Online {
		return candidate.Online{Source: reader, Grades: grades.Service{Source: reader, Times: times, Clock: w.Clock, Limits: limits}, Timetable: timetable.Service{Source: reader, Times: times, Clock: w.Clock, Limits: limits}, Clock: w.Clock, Limits: limits}
	}
	checks := func(reads read.Matcher) candidate.StorageChecks {
		return candidate.StorageChecks{Target: targets, Source: reads, Indexes: reader, Clock: w.Clock, Limits: limits, BackgroundTimeout: c.BuildCheckTimeout}
	}
	builder := candidate.Builder{Ledger: store, Source: targets, Writer: writer, BatchSize: 500}
	validator := candidate.Validator{Store: store, Checks: checks(candidateReads), Queries: online(candidateReads), Clock: w.Clock}
	proof, ok := r.Database.(coord.ExecutionProof)
	if !ok {
		return update.ErrInitialization
	}
	coordinator := &coord.Coordinator{Store: r.Control, Proof: proof, Clock: w.Clock, Random: rand.Reader, ProcessID: r.ProcessID}
	publishedReads := read.Repository{Storage: reader, Gate: coordinator}
	readability := update.Readability{Admission: coordinator, Checks: checks(publishedReads), Queries: online(publishedReads), Store: store, Clock: w.Clock}
	shifter := update.Shifter{Store: store, Outbox: repo, Delivery: delivery, Clock: w.Clock}
	journal := update.Journal{Store: repo, Clock: w.Clock}
	changed := update.Changed{Store: store, Builder: builder, Validator: validator, Readability: readability, Shifter: shifter, Outbox: repo, Delivery: delivery, Progress: repo, Clock: w.Clock, Journal: journal}
	zone, _ := time.LoadLocation(c.Service.Timezone)
	if resume && task.Mode != "initialize" {
		_, e := (update.LocalRecovery{Store: journal, Files: files, Targets: targets, Changed: changed, Zone: zone}).Run(ctx, task)
		if errors.Is(e, sql.ErrNoRows) {
			return errors.Join(update.ErrReacquire, e)
		}
		return e
	}
	var full update.FullSource
	var acquirer update.Acquirer
	var sourceEpoch time.Time
	if resume {
		if task.Mode == "initialize" {
			at, err := repo.SourceEpoch(ctx, task.ID)
			if err != nil {
				return err
			}
			if at != nil {
				sourceEpoch = *at
			}
		}
		m, e := store.Manifest(ctx, options.Combination)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return errors.Join(update.ErrReacquire, e)
			}
			return e
		}
		full = retained{files, m, task.ID}
	} else {
		sourceClient, ok := r.Source.(*adapters.Source)
		if !ok {
			return update.ErrInitialization
		}
		limiter, e := ingest.NewLimiter(c.SourcePageConcurrency)
		if e != nil {
			return e
		}
		collector, e := ingest.NewCoordinator(sourceClient.Client, ingest.BudgetConfig{RequestTimeout: c.DependencyTimeout, PageTimeout: time.Minute, InitialBackoff: time.Second, MaxBackoff: 10 * time.Second, MaxAttempts: 3, RetryHTTP: []int{408, 429, 500, 502, 503, 504}}, ingest.SystemTimer{}, rand.Reader, repo, limiter)
		if e != nil {
			return e
		}
		budget, e := ingest.NewTaskBudget(w.Clock.Now().Add(time.Hour), 100000)
		if e != nil {
			return e
		}
		acquirer = update.Acquirer{Collector: collector, Staging: files, Budget: budget, Limits: ingest.PageLimits{MaxPages: 10000, MaxRecords: 10000000, MaxBytes: c.StagingBytes}, PageSize: c.SourcePageSize, Owner: task.ID, Observe: w.Observe}
		acquirer.PageConcurrency = c.SourcePageConcurrency
		acquirer.Progress = func(ctx context.Context, tables []control.TableAcquisitionProgress) error {
			return repo.SaveAcquisitionProgress(ctx, control.AcquisitionProgress{Tables: tables, UpdatedAt: w.Clock.Now()})
		}
		if c.MailEnabled() {
			consumer, e := notification.NewConsumer(repo, r.Sanitizer, c.Environment)
			if e != nil {
				return e
			}
			acquirer.Notify = func(ctx context.Context, f *ingest.FailureEvent) error { _, _, e := consumer.Consume(ctx, f); return e }
		}
		sourceEpoch, e = w.configureTables(ctx, task, repo, files, &acquirer)
		if e != nil {
			return e
		}
		full = acquirer
	}
	if task.Mode == "initialize" {
		_, e := (update.Initializer{Store: store, Source: full, Preparer: targets, Options: options, Builder: builder, Validator: validator, Outbox: repo, Delivery: delivery, Clock: w.Clock, Progress: repo}).Run(ctx, update.InitialTask{ID: task.ID, Combination: options.Combination, StartedAt: *task.StartedAt, SourceStartedAt: sourceEpoch})
		return e
	}
	active, exists, e := r.Active.Read(ctx)
	if e != nil {
		return e
	}
	if !exists {
		return control.ErrInitializeRequired
	}
	base, e := store.Manifest(ctx, active.Position.BundleID)
	if e != nil {
		return e
	}
	options.Base = &base
	options.BaseSource = targets
	plan, e := repo.PlanUpdate(ctx, active, control.IncrementalOverlap)
	if e != nil {
		return e
	}
	if plan.Repair {
		history, baseline, err := fullUpdateAncestors(ctx, task, plan, repo, r.Files.Staging)
		if err != nil {
			return err
		}
		if baseline {
			if err := targets.Validate(ctx, base); err != nil {
				return err
			}
		}
		acquirer.Reuse = func(ctx context.Context, request ingest.PageRequest, observe staging.TableCopyObserver) (int64, error) {
			pages, err := history.copyTable(ctx, request, files, acquirer.Limits, observe)
			if errors.Is(err, staging.ErrIncomplete) && baseline && !history.selected(request.Table) {
				return update.CopyBaselineTableWithProgress(ctx, targets, base, request, files, acquirer.Limits, observe)
			}
			return pages, err
		}
	} else if acquirer.OnlyTable != "" {
		if err := targets.Validate(ctx, base); err != nil {
			return err
		}
		acquirer.Reuse = func(ctx context.Context, r ingest.PageRequest, observe staging.TableCopyObserver) (int64, error) {
			return update.CopyBaselineTableWithProgress(ctx, targets, base, r, files, acquirer.Limits, observe)
		}
	}
	if e = store.ReopenPublished(ctx, plan, func(ctx context.Context, old update.Copy, gate read.Gate, admission update.FixedAdmission) error {
		reads := read.Repository{Storage: reader, Gate: gate}
		v := update.Readability{Admission: admission, Checks: checks(reads), Queries: online(reads), Clock: w.Clock}
		_, e := v.VerifyPublished(ctx, plan, old)
		return e
	}); e != nil {
		return e
	}
	fetcher := update.Fetcher{Planner: repo, Source: acquirer, Preparer: targets, Options: options, Overlap: control.IncrementalOverlap, Zone: zone}
	unchanged := update.Unchanged{Copies: store, Verifier: readability, Progress: repo, Clock: w.Clock}
	_, e = (update.Runner{Source: fetcher, Unchanged: unchanged, Changed: changed}).Run(ctx, active)
	return e
}

func targetPreparation(c config.Runtime, files *staging.QuotaFiles, combination string) (candidate.TargetFiles, candidate.PrepareOptions) {
	targets := candidate.TargetFiles{Files: files, MaxChunkBytes: 8 << 20, MaxChunks: 100000}
	const rows = 1000
	// Merge counts canonical JSON bytes; target chunks append a newline per row.
	// Reserve framing inside the same limit used by the persistent file writer.
	// A retained seal may use a larger page size from before a configuration
	// change. Keep every supported input page readable during local recovery.
	options := candidate.PrepareOptions{Combination: combination, Scope: c.SourceScope, SpoolDirectory: c.SpoolDirectory, Quality: c.Quality, Limits: merge.Limits{BatchRows: rows, FanIn: 8, MaxRuns: 100000, BatchBytes: targets.MaxChunkBytes - rows, MaxSpoolBytes: c.SpoolBytes, InputPageRows: config.MaxSourcePageSize, InputPageBytes: 64 << 20}}
	options.TableConcurrency = c.BuildTableConcurrency
	return targets, options
}
