package update

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type RepairStore interface {
	IsolateRepair(context.Context, control.ActiveMetadata, control.SlotIdentity) error
	PlanRepair(context.Context, control.ActiveMetadata, control.SlotIdentity, string) (source.Target, error)
	OpenRepaired(context.Context, control.ActiveMetadata, source.Target) error
}
type Repairer struct {
	Store     RepairStore
	Source    candidate.CompleteSource
	Builder   CopyBuilder
	Validator CopyValidator
}

// Repair preserves the published business combination. Its dependencies cannot
// publish a new bundle or advance successful source progress. Isolation commits
// before target validation/build; failure never reopens the damaged position.
func (r Repairer) Repair(ctx context.Context, active control.ActiveMetadata, damaged Copy, candidateID string) (Copy, error) {
	if r.Store == nil || r.Source == nil || r.Builder == nil || r.Validator == nil || active.Validate() != nil || damaged.Manifest.Validate() != nil || damaged.Manifest.Combination != active.Position.BundleID || damaged.Target.Position.Validate() != nil || damaged.Target.Position.Instance == active.Position.Position.Instance || damaged.Target.Generation < 1 || candidateID == "" {
		return Copy{}, ErrInitialization
	}
	h := control.SlotIdentity{Position: damaged.Target.Position, Generation: damaged.Target.Generation, BundleID: damaged.Manifest.Combination}
	if err := r.Store.IsolateRepair(ctx, active, h); err != nil {
		return Copy{}, err
	}
	if err := r.Source.Validate(ctx, damaged.Manifest); err != nil {
		return Copy{}, err
	}
	target, err := r.Store.PlanRepair(ctx, active, h, candidateID)
	if err != nil {
		return Copy{}, err
	}
	if target.Position != h.Position || target.Generation <= h.Generation || target.CandidateID != candidateID {
		return Copy{}, ErrRecoveryPending
	}
	report, err := r.Builder.Build(ctx, target, damaged.Manifest, true)
	if err != nil {
		return Copy{}, err
	}
	if !report.StorageComplete {
		return Copy{}, ErrInitialization
	}
	evidence, err := r.Validator.Validate(ctx, target, damaged.Manifest)
	if err != nil {
		return Copy{}, err
	}
	if err = evidence.Validate(target, damaged.Manifest); err != nil {
		return Copy{}, err
	}
	if err = r.Store.OpenRepaired(ctx, active, target); err != nil {
		return Copy{}, err
	}
	return Copy{Target: target, Manifest: damaged.Manifest, Evidence: evidence}, nil
}
