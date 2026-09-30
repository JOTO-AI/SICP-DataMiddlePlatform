package observability

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type FailedAttempt struct {
	At           time.Time            `json:"at"`
	Source       string               `json:"source"`
	Page         int                  `json:"page"`
	Attempt      int                  `json:"attempt"`
	Failure      string               `json:"failure"`
	HTTPStatus   *int                 `json:"http_status,omitempty"`
	BusinessCode *int64               `json:"business_code,omitempty"`
	DurationNS   *int64               `json:"duration_ns,omitempty"`
	Response     *diagnostic.Response `json:"response,omitempty"`
}

type TaskDiagnostics struct {
	Repository Repository
	Sanitizer  *Sanitizer
}

// FailedAttempts reads the last twenty failed attempts, including retries.
// It never returns raw envelopes, credentials or partial results on DB failure.
func (d TaskDiagnostics) FailedAttempts(ctx context.Context, taskID string) ([]FailedAttempt, error) {
	r, s := d.Repository, d.Sanitizer
	if s == nil || taskID == "" || len(taskID) > 128 || r.Database == nil {
		return nil, ErrPayload
	}
	tx, err := r.Database.Begin(ctx, r.Registry.Logs())
	if err != nil {
		return nil, ErrDelivery
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	query, ok := tx.(storage.QueryExecutor)
	if !ok {
		return nil, ErrDelivery
	}
	id := s.Correlation(taskID)
	rows, err := query.Query(ctx, "SELECT payload::text FROM logs.events WHERE payload #>> '{event,identity,task_id}'=$1 AND kind='attempt' AND phase='finished' AND payload #>> '{event,outcome}'='failed' ORDER BY occurred_at DESC,event_id DESC LIMIT 20", id)
	if err != nil {
		return nil, ErrDelivery
	}
	defer rows.Close()
	out := []FailedAttempt{}
	for rows.Next() {
		var body string
		if len(out) >= 20 || rows.Scan(&body) != nil {
			return nil, ErrDelivery
		}
		safe, err := s.restore([]byte(body))
		if err != nil {
			return nil, err
		}
		e := safe.Event()
		if e.Identity.TaskID != id || e.Kind != events.Attempt || e.Phase != events.Finished || e.Outcome != events.Failed {
			return nil, ErrPayload
		}
		out = append(out, FailedAttempt{At: e.OccurredAt, Source: e.Metadata.Source, Page: e.Metadata.Page, Attempt: e.Identity.AttemptNo, Failure: e.Metadata.Failure, HTTPStatus: e.HTTPStatus, BusinessCode: e.BusinessCode, DurationNS: e.DurationNS, Response: e.Response})
	}
	if rows.Err() != nil {
		return nil, ErrDelivery
	}
	rows.Close()
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return nil, ErrDelivery
	}
	return out, nil
}
