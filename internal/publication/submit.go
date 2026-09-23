// Package publication delivers committed control events to active metadata.
package publication

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type Outbox interface {
	EnqueuePublication(context.Context, control.PublicationIntent) (control.Publication, bool, error)
}
type Delivery interface {
	Apply(context.Context, control.Publication) error
}

// Submit preserves the commit-before-network boundary. Delivery success is not
// complete sync success: application and progress confirmation are later steps.
func Submit(ctx context.Context, outbox Outbox, delivery Delivery, intent control.PublicationIntent) (control.Publication, error) {
	if outbox == nil || delivery == nil {
		return control.Publication{}, control.ErrEntity
	}
	event, _, err := outbox.EnqueuePublication(ctx, intent)
	if err != nil {
		return control.Publication{}, err
	}
	if event.Status == "confirmed" {
		return event, nil
	}
	if event.Status != "pending" {
		return event, control.ErrEntity
	}
	if err = ctx.Err(); err != nil {
		return event, err
	}
	return event, delivery.Apply(ctx, event)
}
