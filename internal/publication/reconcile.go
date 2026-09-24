package publication

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type ActiveReader interface {
	Read(context.Context) (control.ActiveMetadata, bool, error)
}
type Reconciler struct {
	Store  DeliveryStore
	Active ActiveReader
	Atomic Delivery
	Clock  ports.Clock
}

// Apply first observes actual full metadata. An already applied target requires
// no Redis write; only the fenced control acknowledgement is repaired. Missing
// previously published metadata is unknown, never permission to overwrite it.
func (r Reconciler) Apply(ctx context.Context, event control.Publication) error {
	if r.Store == nil || r.Active == nil || r.Atomic == nil || r.Clock == nil {
		return control.ErrEntity
	}
	current, err := r.Store.AuthorizePublication(ctx, event)
	if err != nil {
		return err
	}
	if current.Status == "confirmed" {
		return nil
	}
	if current.Status != "pending" {
		return control.ErrEntity
	}
	target, err := control.DecodeActive(current.Target)
	if err != nil {
		return err
	}
	if target.EventID != current.ID || target.TaskID != current.TaskID || target.Sequence != current.Sequence {
		return control.ErrEntity
	}
	active, present, err := r.Active.Read(ctx)
	if err != nil {
		return err
	}
	if present && active == target {
		return r.Store.RecordPublicationApplied(ctx, current, r.Clock.Now())
	}
	if current.AppliedAt != nil {
		return ErrUnknown
	}
	if current.Expected == nil {
		if present {
			return ErrConflict
		}
	} else {
		expected, err := control.DecodeActive(*current.Expected)
		if err != nil {
			return err
		}
		if !present {
			return ErrUnknown
		}
		if active != expected {
			return ErrConflict
		}
	}
	if err = r.Atomic.Apply(ctx, current); err != nil {
		return err
	}
	return r.Store.RecordPublicationApplied(ctx, current, r.Clock.Now())
}
