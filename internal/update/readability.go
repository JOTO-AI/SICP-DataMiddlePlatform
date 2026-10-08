package update

import (
	"context"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrOldCopy = errors.New("published copy is not verified readable")

type Copy struct {
	Target   source.Target
	Manifest candidate.Manifest
	Evidence candidate.Evidence
}
type FixedAdmission interface {
	AcquireFixed(context.Context, read.Handle) (read.RequestLease, error)
}
type ReadabilityProof struct {
	TaskID      string
	Origin      control.ActiveMetadata
	Destination control.SlotIdentity
	Candidate   source.Target
	Combination string
	CheckedAt   time.Time
}

// RecordReadability must fence the task, recheck both physical identities and
// their complete manifests/evidence, and persist the proof before returning.
// It must not change admission, candidate readiness or the active route.
type ReadabilityStore interface {
	RecordReadability(context.Context, ReadabilityProof, Copy, Copy) error
}
type Readability struct {
	Admission FixedAdmission
	Checks    candidate.Checks
	Queries   candidate.Queries
	Store     ReadabilityStore
	Clock     ports.Clock
}

// Check verifies the OLD published copy independently of the ready NEW copy.
// It uses ordinary read admission and the query principal, never the candidate
// validation path that revokes readiness on unpublished replacement slots.
func (v Readability) Check(ctx context.Context, p control.UpdatePlan, next, old Copy) (proof ReadabilityProof, err error) {
	if v.Admission == nil || v.Checks == nil || v.Queries == nil || v.Store == nil || v.Clock == nil || p.Validate() != nil {
		return proof, ErrOldCopy
	}
	if next.Target.Position.Validate() != nil || next.Target.Generation < 1 || next.Target.CandidateID == "" || next.Target.Position.Instance != p.First || next.Manifest.Combination == p.Origin.Position.BundleID || next.Evidence.Validate(next.Target, next.Manifest) != nil {
		return proof, ErrOldCopy
	}
	if old.Target.Position.Validate() != nil || old.Target.Generation < 1 || old.Target.CandidateID == "" || old.Target.Position.Instance != p.First || old.Target.Position == next.Target.Position || old.Manifest.Combination != p.Origin.Position.BundleID || old.Evidence.Validate(old.Target, old.Manifest) != nil {
		return proof, ErrOldCopy
	}
	at, err := v.VerifyPublished(ctx, p, old)
	if err != nil {
		return proof, err
	}
	proof = ReadabilityProof{TaskID: p.TaskID, Origin: p.Origin, Destination: control.SlotIdentity{Position: old.Target.Position, Generation: old.Target.Generation, BundleID: old.Manifest.Combination}, Candidate: next.Target, Combination: next.Manifest.Combination, CheckedAt: at}
	if err = v.Store.RecordReadability(ctx, proof, next, old); err != nil {
		return ReadabilityProof{}, err
	}
	return proof, nil
}

// VerifyPublished performs ordinary pinned reads without changing readiness or
// publication state. It is shared by old-copy transfer checks and no-change rounds.
func (v Readability) VerifyPublished(ctx context.Context, p control.UpdatePlan, old Copy) (checked time.Time, err error) {
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "old_copy_check", Combination: old.Manifest.Combination, Candidate: old.Target.CandidateID, Slot: old.Target.Position.Slot, Generation: old.Target.Generation}, string(old.Target.Position.Instance))
	defer func() { end(err) }()
	if v.Admission == nil || v.Checks == nil || v.Queries == nil || v.Clock == nil || p.Validate() != nil || old.Target.Position.Validate() != nil || old.Target.Generation < 1 || old.Target.CandidateID == "" || old.Manifest.Combination != p.Origin.Position.BundleID || old.Evidence.Validate(old.Target, old.Manifest) != nil {
		return checked, ErrOldCopy
	}
	h := read.Handle{Position: old.Target.Position, Generation: old.Target.Generation, Combination: old.Manifest.Combination}
	lease, err := v.Admission.AcquireFixed(ctx, h)
	if err != nil {
		return checked, err
	}
	if lease == nil {
		return checked, ErrOldCopy
	}
	closed := false
	defer func() {
		if closed {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, lease.Close(cleanup))
		if err != nil {
			checked = time.Time{}
		}
	}()
	if lease.Handle() != h {
		return checked, ErrOldCopy
	}
	if binding, ok := lease.(interface {
		Context(context.Context) context.Context
	}); ok {
		ctx = binding.Context(ctx)
	}
	if err = v.Checks.SourceStorage(ctx, h, old.Manifest); err != nil {
		return checked, err
	}
	if err = v.Checks.SourceIndexes(ctx, h, old.Manifest); err != nil {
		return checked, err
	}
	at := v.Clock.Now()
	if at.Before(p.StartedAt) {
		return checked, control.ErrTime
	}
	queries, closeSession, err := candidate.QuerySession(ctx, v.Queries, h, old.Manifest)
	if err != nil {
		return checked, err
	}
	defer closeSession()
	for _, def := range contract.Definitions() {
		count, digest, e := queries.Check(ctx, h, def.Operation, at)
		if e != nil {
			return checked, e
		}
		// Reuse the evidence validator to check the exact digest format and
		// table dependencies without trusting a bare success flag.
		q := old.Evidence.Queries[def.Operation]
		q.Cases = count
		q.SHA256 = digest
		evidence := old.Evidence
		evidence.Queries = map[contract.Operation]candidate.QueryEvidence{}
		for op, value := range old.Evidence.Queries {
			evidence.Queries[op] = value
		}
		evidence.Queries[def.Operation] = q
		if evidence.Validate(old.Target, old.Manifest) != nil {
			return checked, ErrOldCopy
		}
	}
	if err = ctx.Err(); err != nil {
		return checked, err
	}
	// A failed release must not leave a persisted successful check behind.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	err = lease.Close(cleanup)
	cancel()
	closed = true
	if err != nil {
		return checked, err
	}
	return at, nil
}
