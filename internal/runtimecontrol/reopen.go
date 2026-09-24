package runtimecontrol

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

// ReopenPublished handles the old standby closed by an earlier failed round's
// drain. Only the current published bundle may be reopened, after real checks
// under the execution and slot locks. It cannot open an unpublished candidate.
func (s Store) ReopenPublished(ctx context.Context, p control.UpdatePlan, verify func(context.Context, update.Copy, read.Gate, update.FixedAdmission) error) error {
	if p.Validate() != nil || p.TaskID != s.Owner.TaskID || verify == nil {
		return control.ErrEntity
	}
	return s.repo().Fenced(ctx, func(tx storage.Transaction) error {
		if _, e := s.task(ctx, tx); e != nil {
			return e
		}
		if e := noPending(ctx, tx); e != nil {
			return e
		}
		a, e := current(ctx, tx)
		if e != nil {
			return e
		}
		if a == nil || *a != p.Origin {
			return control.ErrEntity
		}
		old, e := publishedCopy(ctx, tx, p.Origin.Position.BundleID, p.First, s.CodeVersion)
		if e != nil {
			return e
		}
		v, e := slot(ctx, tx, old.Target.Position)
		if e != nil {
			return e
		}
		if v.AdmissionOpen {
			return nil
		}
		if v.Generation != old.Target.Generation || v.BundleID == nil || *v.BundleID != p.Origin.Position.BundleID || v.State != "ready" || old.Target.Position.Instance == a.Position.Position.Instance {
			return control.ErrEntity
		}
		gate := lockedReads{read.Handle{Position: old.Target.Position, Generation: old.Target.Generation, Combination: old.Manifest.Combination}}
		if e = verify(ctx, old, gate, gate); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		return one(tx.Exec(ctx, "UPDATE control.slots SET admission_open=TRUE WHERE instance=$1 AND slot=$2", v.Instance, v.Slot))
	})
}

// These leases exist only inside the synchronous callback above; its enclosing
// control transaction owns exclusion for the entire query-role verification.
type lockedReads struct{ h read.Handle }

func (l lockedReads) Handle() read.Handle           { return l.h }
func (l lockedReads) Close(context.Context) error   { return nil }
func (l lockedReads) Release(context.Context) error { return nil }
func (l lockedReads) BeginRead(_ context.Context, h read.Handle) (read.SQLLease, error) {
	if h != l.h {
		return nil, read.ErrHandle
	}
	return l, nil
}
func (l lockedReads) AcquireFixed(_ context.Context, h read.Handle) (read.RequestLease, error) {
	if h != l.h {
		return nil, read.ErrHandle
	}
	return l, nil
}
