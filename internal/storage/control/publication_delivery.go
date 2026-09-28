package control

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func samePublication(a, b Publication) bool {
	aTarget, e1 := DecodeActive(a.Target)
	bTarget, e2 := DecodeActive(b.Target)
	return e1 == nil && e2 == nil && aTarget == bTarget && a.ID == b.ID && a.Sequence == b.Sequence && a.TaskID == b.TaskID && a.Kind == b.Kind && a.CreatedAt.Equal(b.CreatedAt) && sameActiveDocument(a.Expected, b.Expected)
}
func (r Repository) publicationForDelivery(ctx context.Context, tx storage.Transaction, offered Publication) (Publication, error) {
	if r.owner == nil {
		return Publication{}, ErrOwnership
	}
	if err := r.fenceWriter(ctx, tx); err != nil {
		return Publication{}, err
	}
	d, _ := describe(&Publication{})
	loaded, err := readEntity(ctx, tx, d, `"id"=$1`, []any{offered.ID}, true)
	if err != nil {
		return Publication{}, err
	}
	current := loaded.Interface().(Publication)
	if current.TaskID != r.owner.TaskID || !samePublication(current, offered) {
		return Publication{}, ErrIdempotency
	}
	target, err := DecodeActive(current.Target)
	if err != nil {
		return Publication{}, err
	}
	if target.Sequence != current.Sequence || target.EventID != current.ID || target.TaskID != current.TaskID {
		return Publication{}, ErrEntity
	}
	if current.Status == "confirmed" {
		return current, nil
	}
	if current.Status != "pending" {
		return Publication{}, ErrEntity
	}
	intent := PublicationIntent{ID: current.ID, Kind: current.Kind, Target: target.Position, CreatedAt: current.CreatedAt}
	if current.Expected != nil {
		expected, err := DecodeActive(*current.Expected)
		if err != nil {
			return Publication{}, err
		}
		intent.Expected = &expected
	}
	if err = intent.validate(); err != nil {
		return Publication{}, err
	}
	if err = publicationTarget(ctx, tx, intent, current.TaskID); err != nil {
		return Publication{}, err
	}
	return current, nil
}

func (r Repository) AuthorizePublication(ctx context.Context, offered Publication) (Publication, error) {
	var current Publication
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		var err error
		current, err = r.publicationForDelivery(ctx, tx, offered)
		return err
	})
	if err != nil {
		return Publication{}, err
	}
	return current, nil
}
func (r Repository) RecordPublicationApplied(ctx context.Context, offered Publication, at time.Time) error {
	if at.IsZero() {
		return ErrTime
	}
	at = at.UTC().Truncate(time.Microsecond)
	return r.transaction(ctx, func(tx storage.Transaction) error {
		current, err := r.publicationForDelivery(ctx, tx, offered)
		if err != nil {
			return err
		}
		if current.Status == "confirmed" || current.AppliedAt != nil {
			return nil
		}
		if at.Before(current.CreatedAt) {
			return ErrTime
		}
		return exactlyOne(tx.Exec(ctx, "UPDATE control.publication_outbox SET applied_at=$2 WHERE id=$1 AND status='pending' AND applied_at IS NULL", current.ID, at))
	})
}
