package update

import (
	"context"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrNotDrained = errors.New("original query instance still has readers or SQL")

type UpdateSource interface {
	Prepare(context.Context, control.ActiveMetadata) (PreparedUpdate, error)
}

// PlanCopies persists the complete target and physical identities, without
// writing either business slot. ConfirmCopies rechecks both current fenced
// readiness records; a returned flag alone is never publication authority.
type CopyStore interface {
	PlanCopies(context.Context, PreparedUpdate) ([2]source.Target, Copy, error)
	ConfirmCopies(context.Context, control.UpdatePlan, [2]Copy) error
}
type OldCopyCheck interface {
	Check(context.Context, control.UpdatePlan, Copy, Copy) (ReadabilityProof, error)
}
type RouteShifter interface {
	Shift(context.Context, ReadabilityProof) (ShiftResult, error)
}
type Changed struct {
	Source      UpdateSource
	Store       CopyStore
	Builder     CopyBuilder
	Validator   CopyValidator
	Readability OldCopyCheck
	Shifter     RouteShifter
	Outbox      publication.Outbox
	Delivery    publication.Delivery
	Progress    ProgressStore
	Clock       ports.Clock
	CodeVersion string
}
type ChangeResult struct {
	Prepared PreparedUpdate
	Copies   [2]Copy
	Reports  [2]candidate.Report
	Shift    ShiftResult
	Event    control.Publication
}

func (c Changed) Run(ctx context.Context, active control.ActiveMetadata) (out ChangeResult, err error) {
	if c.Source == nil {
		return out, ErrInitialization
	}
	prepared, err := c.Source.Prepare(ctx, active)
	if err != nil {
		return out, err
	}
	return c.RunPrepared(ctx, prepared)
}

func (c Changed) RunPrepared(ctx context.Context, prepared PreparedUpdate) (out ChangeResult, err error) {
	if c.Store == nil || c.Builder == nil || c.Validator == nil || c.Readability == nil || c.Shifter == nil || c.Outbox == nil || c.Delivery == nil || c.Progress == nil || c.Clock == nil || c.CodeVersion == "" {
		return out, ErrInitialization
	}
	out.Prepared = prepared
	p := out.Prepared.Plan
	m := out.Prepared.Manifest
	if p.Validate() != nil || len(p.TaskID) > 64 || m.Validate() != nil || m.Combination == p.Origin.Position.BundleID {
		return out, ErrInitialization
	}
	targets, old, err := c.Store.PlanCopies(ctx, out.Prepared)
	if err != nil {
		return out, err
	}
	for n, t := range targets {
		instance := p.First
		if n == 1 {
			instance = p.Second
		}
		if t.Position.Validate() != nil || t.Position.Instance != instance || t.Generation < 1 || t.CandidateID == "" {
			return out, ErrInitialization
		}
		out.Copies[n] = Copy{Target: t, Manifest: m}
	}
	build := func(n int) error {
		var e error
		out.Reports[n], e = c.Builder.Build(ctx, targets[n], m, p.Mode == "force_full")
		if e != nil {
			return e
		}
		if !out.Reports[n].StorageComplete {
			return ErrInitialization
		}
		out.Copies[n].Evidence, e = c.Validator.Validate(ctx, targets[n], m)
		if e != nil {
			return e
		}
		return out.Copies[n].Evidence.Validate(targets[n], m, c.CodeVersion)
	}
	if err = build(0); err != nil {
		return out, err
	}
	proof, err := c.Readability.Check(ctx, p, out.Copies[0], old)
	if err != nil {
		return out, err
	}
	out.Shift, err = c.Shifter.Shift(ctx, proof)
	if err != nil {
		return out, err
	}
	if !out.Shift.Drained {
		return out, ErrNotDrained
	}
	// Pin the acknowledged shifted value, never reread a mutable live route.
	shifted, err := control.DecodeActive(out.Shift.Event.Target)
	if err != nil {
		return out, err
	}
	if out.Shift.Event.Status != "confirmed" || out.Shift.Event.Kind != "route_shift" || shifted.Position != proof.Destination || shifted.TaskID != p.TaskID {
		return out, ErrInitialization
	}
	if err = build(1); err != nil {
		return out, err
	}
	if err = c.Store.ConfirmCopies(ctx, p, out.Copies); err != nil {
		return out, err
	}
	progress := control.ProgressPlan{TaskID: p.TaskID, BundleID: m.Combination, Mode: p.Mode, StartedAt: p.StartedAt, CheckedAt: c.Clock.Now(), Ends: map[string]time.Time{}}
	for _, table := range m.Tables {
		progress.Ends[table.Table] = p.StartedAt
	}
	if err = c.Progress.StageProgress(ctx, progress); err != nil {
		return out, err
	}
	intent := control.PublicationIntent{ID: p.TaskID + "-publish", Kind: "bundle_publish", CodeVersion: c.CodeVersion, Expected: &shifted, Target: control.SlotIdentity{Position: targets[0].Position, Generation: targets[0].Generation, BundleID: m.Combination}, CreatedAt: c.Clock.Now()}
	out.Event, err = publication.Submit(ctx, c.Outbox, c.Delivery, intent)
	if err != nil {
		return out, err
	}
	confirmed, e := c.Progress.ConfirmPublication(ctx, out.Event, c.Clock.Now())
	if e != nil {
		return out, e
	}
	out.Event = confirmed
	err = c.Progress.CompletePublished(ctx, out.Event, c.Clock.Now())
	return out, err
}
