package observability

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type Projection struct {
	EntityID                                   string
	Kind                                       events.Kind
	Identity                                   events.Identity
	StartedAt, FinishedAt, TransportFinishedAt *time.Time
	Business, Transport                        events.Outcome
	BusinessHTTPStatus, TransportHTTPStatus    *int
}

// Project rebuilds state from immutable persisted facts. Arrival order and PG
// received_at never overwrite occurred_at or turn completed work back to running.
func Project(batch []SafeEvent) (Projection, error) {
	if len(batch) == 0 || len(batch) > 1000 {
		return Projection{}, ErrPayload
	}
	var p Projection
	var start, finish, transport *events.Event
	seen := map[string]string{}
	for _, safe := range batch {
		if !safe.Valid() {
			return Projection{}, ErrPayload
		}
		e := safe.Event()
		if old, ok := seen[safe.ID()]; ok && old != safe.body {
			return Projection{}, ErrConflict
		}
		seen[safe.ID()] = safe.body
		if p.EntityID == "" {
			p.EntityID = e.EntityID
			p.Kind = e.Kind
			p.Identity = e.Identity
		}
		if e.EntityID != p.EntityID || e.Kind != p.Kind || e.Identity != p.Identity {
			return Projection{}, ErrPayload
		}
		switch e.Phase {
		case events.Started:
			if start == nil || e.OccurredAt.Before(start.OccurredAt) || e.OccurredAt.Equal(start.OccurredAt) && e.ID < start.ID {
				copy := e
				start = &copy
			}
		case events.Finished:
			if later(e, finish) {
				copy := e
				finish = &copy
			}
		case events.Transport:
			if later(e, transport) {
				copy := e
				transport = &copy
			}
		}
	}
	if start != nil {
		at := start.OccurredAt
		p.StartedAt = &at
	}
	if finish != nil {
		at := finish.OccurredAt
		p.FinishedAt = &at
		p.Business = finish.Outcome
		p.BusinessHTTPStatus = finish.HTTPStatus
	}
	if transport != nil {
		at := transport.OccurredAt
		p.TransportFinishedAt = &at
		p.Transport = transport.Outcome
		p.TransportHTTPStatus = transport.HTTPStatus
	}
	return p, nil
}
func later(e events.Event, old *events.Event) bool {
	return old == nil || e.OccurredAt.After(old.OccurredAt) || e.OccurredAt.Equal(old.OccurredAt) && e.ID > old.ID
}

// ReadProjection maps the raw control/entity identity once, then reads original
// facts from the fixed logs database; no in-process projection state is required.
func (r Repository) ReadProjection(ctx context.Context, s *Sanitizer, rawEntityID string) (Projection, error) {
	if s == nil || rawEntityID == "" || len(rawEntityID) > 128 || r.Database == nil || r.Registry.Logs().DSN() == "" {
		return Projection{}, ErrPayload
	}
	source := r.Source
	if source == "" {
		if r.Registry.LogMode() != storage.SingleLegacy {
			return Projection{}, ErrPayload
		}
		source = storage.PlatformLog
	}
	connection, err := r.Registry.LogConnection(source)
	if err != nil {
		return Projection{}, ErrPayload
	}
	tx, err := r.Database.Begin(ctx, connection)
	if err != nil {
		return Projection{}, ErrDelivery
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	query, ok := tx.(storage.QueryExecutor)
	if !ok {
		return Projection{}, ErrDelivery
	}
	rows, err := query.Query(ctx, "SELECT payload::text FROM logs.events WHERE entity_id=$1 ORDER BY occurred_at,event_id LIMIT 1001", s.Correlation(rawEntityID))
	if err != nil {
		return Projection{}, ErrDelivery
	}
	defer rows.Close()
	batch := []SafeEvent{}
	totalBytes := 0
	for rows.Next() {
		if len(batch) == 1000 {
			return Projection{}, ErrLimit
		}
		var body string
		if rows.Scan(&body) != nil {
			return Projection{}, ErrDelivery
		}
		if len(body) > (64<<20)-totalBytes {
			return Projection{}, ErrLimit
		}
		totalBytes += len(body)
		safe, err := s.restore([]byte(body))
		if err != nil {
			return Projection{}, err
		}
		batch = append(batch, safe)
	}
	if rows.Err() != nil {
		return Projection{}, ErrDelivery
	}
	rows.Close()
	p, err := Project(batch)
	if err != nil {
		return Projection{}, err
	}
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return Projection{}, ErrDelivery
	}
	return p, nil
}
