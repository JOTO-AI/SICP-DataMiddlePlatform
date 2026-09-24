package update

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

// AuthorizeShift rechecks the persisted old-copy proof under execution fencing.
// It never authorizes a proof for a different task, generation or new candidate.
type ShiftStore interface {
	AuthorizeShift(context.Context, ReadabilityProof) error
	ConfirmPublication(context.Context, control.Publication, time.Time) (control.Publication, error)
	DrainShift(context.Context, control.Publication) (bool, error)
}
type Shifter struct {
	Store    ShiftStore
	Outbox   publication.Outbox
	Delivery publication.Delivery
	Clock    ports.Clock
}
type ShiftResult struct {
	Event   control.Publication
	Drained bool
}

// Shift keeps the durable event even on an unknown reply. No SQL drain or
// candidate write is attempted until the route is durably confirmed.
func (s Shifter) Shift(ctx context.Context, p ReadabilityProof) (out ShiftResult, err error) {
	if s.Store == nil || s.Outbox == nil || s.Delivery == nil || s.Clock == nil || p.TaskID == "" || len(p.TaskID) > 64 || p.Origin.Validate() != nil || p.Destination.Position.Validate() != nil || p.Destination.Generation < 1 || p.Destination.BundleID != p.Origin.Position.BundleID || p.Destination.Position.Instance == p.Origin.Position.Position.Instance || p.Candidate.Position.Instance != p.Destination.Position.Instance || p.Candidate.Position == p.Destination.Position || p.Candidate.Generation < 1 || p.Candidate.CandidateID == "" || p.Combination == "" || p.Combination == p.Destination.BundleID || p.CodeVersion == "" || p.CheckedAt.IsZero() {
		return out, ErrOldCopy
	}
	at := s.Clock.Now()
	if at.Before(p.CheckedAt) {
		return out, control.ErrTime
	}
	if err = s.Store.AuthorizeShift(ctx, p); err != nil {
		return out, err
	}
	intent := control.PublicationIntent{ID: p.TaskID + "-shift", Kind: "route_shift", CodeVersion: p.CodeVersion, Expected: &p.Origin, Target: p.Destination, CreatedAt: at}
	out.Event, err = publication.Submit(ctx, s.Outbox, s.Delivery, intent)
	if err != nil {
		return out, err
	}
	confirmed, e := s.Store.ConfirmPublication(ctx, out.Event, s.Clock.Now())
	if e != nil {
		return out, e
	}
	out.Event = confirmed
	if confirmed.Kind != "route_shift" || confirmed.Status != "confirmed" || confirmed.ConfirmedAt == nil {
		return out, control.ErrEntity
	}
	out.Drained, err = s.Store.DrainShift(ctx, confirmed)
	return out, err
}
