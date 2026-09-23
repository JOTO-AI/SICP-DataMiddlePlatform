package publication

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type DeliveryStore interface {
	AuthorizePublication(context.Context, control.Publication) (control.Publication, error)
	RecordPublicationApplied(context.Context, control.Publication, time.Time) error
}
type Deliverer struct {
	Store  DeliveryStore
	Atomic Delivery
	Clock  ports.Clock
}

func (d Deliverer) Apply(ctx context.Context, event control.Publication) error {
	if d.Store == nil || d.Atomic == nil || d.Clock == nil {
		return control.ErrEntity
	}
	current, err := d.Store.AuthorizePublication(ctx, event)
	if err != nil {
		return err
	}
	if current.Status == "confirmed" {
		return nil
	}
	if current.Status != "pending" {
		return control.ErrEntity
	}
	if err = d.Atomic.Apply(ctx, current); err != nil {
		return err
	}
	// An acknowledged Redis application is a separate fact from formal publish
	// and successful source progress. Any error above leaves the event pending.
	return d.Store.RecordPublicationApplied(ctx, current, d.Clock.Now())
}
