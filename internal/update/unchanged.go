package update

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func SameContent(a, b candidate.Manifest) bool {
	if a.Validate() != nil || b.Validate() != nil {
		return false
	}
	tables := map[string]candidate.TableVersion{}
	for _, v := range a.Tables {
		tables[v.Table] = v
	}
	for _, v := range b.Tables {
		if tables[v.Table] != v {
			return false
		}
	}
	return true
}

type PublishedCopies interface {
	LoadPublished(context.Context, control.UpdatePlan) ([2]Copy, error)
}
type PublishedVerifier interface {
	VerifyPublished(context.Context, control.UpdatePlan, Copy) (time.Time, error)
}
type UnchangedProgress interface {
	StageProgress(context.Context, control.ProgressPlan) error
	CompleteUnchanged(context.Context, control.ActiveMetadata, [2]control.SlotIdentity, time.Time) error
}
type Unchanged struct {
	Copies   PublishedCopies
	Verifier PublishedVerifier
	Progress UnchangedProgress
	Clock    ports.Clock
}

// Try treats a complete target equal to the old content as unchanged only if
// both physical copies pass full read checks. It has no publisher/cache writer.
func (u Unchanged) Try(ctx context.Context, in PreparedUpdate) (bool, error) {
	if u.Copies == nil || u.Verifier == nil || u.Progress == nil || u.Clock == nil || in.Plan.Validate() != nil || in.Manifest.Validate() != nil {
		return false, ErrInitialization
	}
	if in.Plan.Mode != "incremental" {
		return false, nil
	}
	copies, err := u.Copies.LoadPublished(ctx, in.Plan)
	if err != nil {
		return false, err
	}
	if copies[0].Manifest.Combination != in.Plan.Origin.Position.BundleID || copies[0].Manifest.Validate() != nil {
		return false, ErrOldCopy
	}
	if !SameContent(in.Manifest, copies[0].Manifest) {
		return false, nil
	}
	var identities [2]control.SlotIdentity
	activity.Report(ctx, "checking_unchanged", "", "")
	checked := in.Plan.StartedAt
	for n, copy := range copies {
		instance := storage.Main
		if n == 1 {
			instance = storage.Standby
		}
		if copy.Target.Position.Instance != instance || copy.Manifest.Combination != in.Plan.Origin.Position.BundleID || !SameContent(copy.Manifest, in.Manifest) || copy.Evidence.Validate(copy.Target, copy.Manifest) != nil {
			return false, ErrOldCopy
		}
		identities[n] = control.SlotIdentity{Position: copy.Target.Position, Generation: copy.Target.Generation, BundleID: copy.Manifest.Combination}
		if instance == in.Plan.Origin.Position.Position.Instance && identities[n] != in.Plan.Origin.Position {
			return false, ErrOldCopy
		}
		at, e := u.Verifier.VerifyPublished(ctx, in.Plan, copy)
		if e != nil {
			return false, e
		}
		if at.Before(in.Plan.StartedAt) {
			return false, control.ErrTime
		}
		if at.After(checked) {
			checked = at
		}
	}
	p := control.ProgressPlan{TaskID: in.Plan.TaskID, BundleID: in.Plan.Origin.Position.BundleID, Mode: in.Plan.Mode, StartedAt: in.Plan.StartedAt, CheckedAt: checked, Ends: map[string]time.Time{}}
	for name := range in.Plan.Windows {
		p.Ends[name] = in.Plan.StartedAt
	}
	p.Summary, err = resultCounts(in, [2]candidate.Report{})
	if err != nil {
		return false, err
	}
	if err = u.Progress.StageProgress(ctx, p); err != nil {
		return false, err
	}
	activity.Report(ctx, "confirming_progress", "", "")
	if err = u.Progress.CompleteUnchanged(ctx, in.Plan.Origin, identities, u.Clock.Now()); err != nil {
		return false, err
	}
	return true, nil
}
