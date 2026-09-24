package update

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type ProgressStore interface {
	StageProgress(context.Context, control.ProgressPlan) error
	ConfirmPublication(context.Context, control.Publication, time.Time) (control.Publication, error)
	CompletePublished(context.Context, control.Publication, time.Time) error
}

// Finisher can resume solely from the durable event ID/payload. In particular it
// has no acquisition, target, or builder dependency, even after lost DB replies.
type Finisher struct {
	Store    ProgressStore
	Delivery publication.Delivery
	Clock    ports.Clock
}

func (f Finisher) Finish(ctx context.Context, event control.Publication) error {
	if f.Store == nil || f.Delivery == nil || f.Clock == nil {
		return ErrInitialization
	}
	if err := f.Delivery.Apply(ctx, event); err != nil {
		return err
	}
	confirmed, err := f.Store.ConfirmPublication(ctx, event, f.Clock.Now())
	if err != nil {
		return err
	}
	return f.Store.CompletePublished(ctx, confirmed, f.Clock.Now())
}

// Recover confirms either kind of immutable event. A route shift never advances
// successful source progress; local recovery must recheck its drain separately.
func (f Finisher) Recover(ctx context.Context, event control.Publication) (control.Publication, error) {
	if f.Store == nil || f.Delivery == nil || f.Clock == nil || (event.Kind != "route_shift" && event.Kind != "bundle_publish") {
		return control.Publication{}, ErrInitialization
	}
	if err := f.Delivery.Apply(ctx, event); err != nil {
		return control.Publication{}, err
	}
	confirmed, err := f.Store.ConfirmPublication(ctx, event, f.Clock.Now())
	if err != nil {
		return control.Publication{}, err
	}
	if confirmed.Kind != event.Kind || confirmed.Status != "confirmed" {
		return control.Publication{}, ErrRecoveryPending
	}
	if event.Kind == "bundle_publish" {
		err = f.Store.CompletePublished(ctx, confirmed, f.Clock.Now())
	}
	return confirmed, err
}
