// Package update serially orchestrates source acquisition, copies and publish.
package update

import (
	"context"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrInitialization = errors.New("initialization prerequisites or copy evidence invalid")

type Acquirer struct {
	Collector *ingest.Coordinator
	Staging   *staging.Store
	Budget    *ingest.TaskBudget
	Limits    ingest.PageLimits
	PageSize  int
	Owner     string
	Observe   func(error)
	Notify    func(context.Context, *ingest.FailureEvent) error
}

// Full commits every page and requires an actual fourteen-source seal. The
// retained lease stays durable on later failure for controlled recovery/GC.
func (a Acquirer) Full(ctx context.Context) (*staging.Lease, error) {
	return a.collect(ctx, func(table string) ingest.PageRequest {
		return ingest.PageRequest{Table: table, Mode: ingest.Full, Page: 1, PageSize: a.PageSize}
	})
}

func (a Acquirer) collect(ctx context.Context, request func(string) ingest.PageRequest) (*staging.Lease, error) {
	if a.Collector == nil || a.Staging == nil || a.Budget == nil || a.PageSize < 1 || a.Owner == "" || a.Observe == nil {
		return nil, ErrInitialization
	}
	for _, table := range ingest.Catalog() {
		result, err := a.Collector.Collect(ctx, request(table.Name), a.Budget, a.Limits, a.Staging)
		if result.FinalFailure != nil && a.Notify != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			notifyErr := a.Notify(cleanup, result.FinalFailure)
			cancel()
			err = errors.Join(err, notifyErr)
		}
		if result.ObservationError != nil {
			a.Observe(result.ObservationError)
		}
		if err != nil {
			return nil, err
		}
		if !result.Manifest.Complete {
			return nil, ErrInitialization
		}
	}
	ref, err := a.Staging.Seal(ctx, a.Limits)
	if err != nil {
		return nil, err
	}
	return a.Staging.Retain(ctx, ref, a.Owner)
}

type InitialTask struct {
	ID, Combination, CodeVersion string
	StartedAt                    time.Time
}

// PlanInitial must confirm no active bundle or unresolved event and persist
// both physical roles under the current execution owner. RecordTarget commits
// the immutable bundle/manifest and candidate identities before any slot write.
type InitialStore interface {
	PlanInitial(context.Context, InitialTask) ([2]source.Target, error)
	RecordTarget(context.Context, InitialTask, [2]source.Target, candidate.Manifest) error
}
type FullSource interface {
	Full(context.Context) (*staging.Lease, error)
}
type TargetPreparer interface {
	Prepare(context.Context, candidate.PrepareOptions, *staging.Lease) (candidate.Manifest, error)
}
type CopyBuilder interface {
	Build(context.Context, source.Target, candidate.Manifest, bool) (candidate.Report, error)
}
type CopyValidator interface {
	Validate(context.Context, source.Target, candidate.Manifest) (candidate.Evidence, error)
}
type Initializer struct {
	Store     InitialStore
	Source    FullSource
	Preparer  TargetPreparer
	Options   candidate.PrepareOptions
	Builder   CopyBuilder
	Validator CopyValidator
	Outbox    publication.Outbox
	Delivery  publication.Delivery
	Clock     ports.Clock
	Progress  ProgressStore
}
type Initialization struct {
	Task     InitialTask
	Manifest candidate.Manifest
	Targets  [2]source.Target
	Evidence [2]candidate.Evidence
	Event    control.Publication
}

func (i Initializer) Run(ctx context.Context, task InitialTask) (out Initialization, err error) {
	if i.Store == nil || i.Source == nil || i.Preparer == nil || i.Builder == nil || i.Validator == nil || i.Outbox == nil || i.Delivery == nil || i.Progress == nil || i.Clock == nil || task.ID == "" || len(task.ID) > 64 || task.Combination == "" || task.CodeVersion == "" || task.StartedAt.IsZero() {
		return out, ErrInitialization
	}
	out.Task = task
	out.Targets, err = i.Store.PlanInitial(ctx, task)
	if err != nil {
		return out, err
	}
	for index, target := range out.Targets {
		instance := storage.Main
		if index == 1 {
			instance = storage.Standby
		}
		if target.Position.Validate() != nil || target.Position.Instance != instance || target.Generation < 1 || target.CandidateID == "" {
			return out, ErrInitialization
		}
	}
	lease, err := i.Source.Full(ctx)
	if err != nil {
		return out, err
	}
	if lease == nil {
		return out, ErrInitialization
	}
	options := i.Options
	options.Combination = task.Combination
	options.Mode = ingest.Full
	options.Base = nil
	options.BaseSource = nil
	out.Manifest, err = i.Preparer.Prepare(ctx, options, lease)
	if err != nil {
		return out, err
	}
	if out.Manifest.Validate() != nil || out.Manifest.Combination != task.Combination || out.Manifest.Input != lease.Reference() {
		return out, ErrInitialization
	}
	if err = i.Store.RecordTarget(ctx, task, out.Targets, out.Manifest); err != nil {
		return out, err
	}
	for n, target := range out.Targets {
		var report candidate.Report
		report, err = i.Builder.Build(ctx, target, out.Manifest, true)
		if err != nil {
			return out, err
		}
		if !report.StorageComplete {
			return out, ErrInitialization
		}
		out.Evidence[n], err = i.Validator.Validate(ctx, target, out.Manifest)
		if err != nil {
			return out, err
		}
		if err = out.Evidence[n].Validate(target, out.Manifest, task.CodeVersion); err != nil {
			return out, err
		}
	}
	plan := control.ProgressPlan{TaskID: task.ID, BundleID: task.Combination, Mode: "initialize", StartedAt: task.StartedAt, CheckedAt: i.Clock.Now(), Ends: map[string]time.Time{}}
	for _, table := range out.Manifest.Tables {
		plan.Ends[table.Table] = task.StartedAt
	}
	if err = i.Progress.StageProgress(ctx, plan); err != nil {
		return out, err
	}
	// No route_shift exists on this path: first publication follows BOTH copies.
	target := out.Targets[1]
	intent := control.PublicationIntent{ID: task.ID + "-publish", Kind: "bundle_publish", CodeVersion: task.CodeVersion, Target: control.SlotIdentity{Position: target.Position, Generation: target.Generation, BundleID: task.Combination}, CreatedAt: i.Clock.Now()}
	out.Event, err = publication.Submit(ctx, i.Outbox, i.Delivery, intent)
	if err != nil {
		return out, err
	}
	confirmed, confirmErr := i.Progress.ConfirmPublication(ctx, out.Event, i.Clock.Now())
	err = confirmErr
	if err != nil {
		return out, err
	}
	out.Event = confirmed
	err = i.Progress.CompletePublished(ctx, out.Event, i.Clock.Now())
	return out, err
}
