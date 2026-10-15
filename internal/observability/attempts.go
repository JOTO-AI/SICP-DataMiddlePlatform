package observability

import (
	"context"
	"sort"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type FailedAttempt struct {
	Diagnostics    []diagnostic.Request       `json:"diagnostics,omitempty"`
	TaskID         string                     `json:"task_id,omitempty"`
	CallID         string                     `json:"call_id,omitempty"`
	AttemptID      string                     `json:"attempt_id,omitempty"`
	Path           string                     `json:"path,omitempty"`
	Mode           string                     `json:"mode,omitempty"`
	PageSize       int                        `json:"page_size,omitempty"`
	StartTime      string                     `json:"start_time,omitempty"`
	EndTime        string                     `json:"end_time,omitempty"`
	Authentication *diagnostic.Authentication `json:"authentication,omitempty"`
	EventID        string                     `json:"event_id"`
	LogSource      storage.LogSource          `json:"log_source"`
	At             time.Time                  `json:"at"`
	Source         string                     `json:"source"`
	Page           int                        `json:"page"`
	Attempt        int                        `json:"attempt"`
	Failure        string                     `json:"failure"`
	HTTPStatus     *int                       `json:"http_status,omitempty"`
	BusinessCode   *int64                     `json:"business_code,omitempty"`
	DurationNS     *int64                     `json:"duration_ns,omitempty"`
	Response       *diagnostic.Response       `json:"response,omitempty"`
}

type TaskDiagnostics struct {
	Repository Repository
	Sanitizer  *Sanitizer
}

type DiagnosticSource struct {
	Source   storage.LogSource `json:"source"`
	Complete bool              `json:"complete"`
	Reason   string            `json:"reason,omitempty"`
}
type DiagnosticReport struct {
	Items    []FailedAttempt    `json:"items"`
	Complete bool               `json:"complete"`
	Sources  []DiagnosticSource `json:"sources"`
}

func (d TaskDiagnostics) FailedAttemptReport(ctx context.Context, taskID string) (DiagnosticReport, error) {
	out := DiagnosticReport{Items: []FailedAttempt{}, Complete: true, Sources: []DiagnosticSource{}}
	if d.Sanitizer == nil || taskID == "" || len(taskID) > 128 || d.Repository.Database == nil {
		return out, ErrPayload
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sources := []storage.LogSource{storage.PlatformLog}
	if d.Repository.Registry.LogMode() == storage.DualLogs {
		sources = append(sources, storage.UpstreamLog)
	}
	type result struct {
		source storage.LogSource
		items  []FailedAttempt
		err    error
	}
	results := make(chan result, len(sources))
	for _, source := range sources {
		go func() { items, err := d.failedAttempts(ctx, taskID, source); results <- result{source, items, err} }()
	}
	bySource := map[storage.LogSource]result{}
	for range sources {
		select {
		case r := <-results:
			bySource[r.source] = r
		case <-ctx.Done():
		}
	}
	for _, source := range sources {
		r, ok := bySource[source]
		state := DiagnosticSource{Source: source, Complete: ok && r.err == nil}
		if !state.Complete {
			out.Complete = false
			state.Reason = "unavailable"
			if r.err == ErrPayload || r.err == ErrConflict {
				state.Reason = "invalid_payload"
			}
		} else {
			out.Items = append(out.Items, r.items...)
		}
		out.Sources = append(out.Sources, state)
	}
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		if a.EventID != b.EventID {
			return a.EventID > b.EventID
		}
		return a.LogSource < b.LogSource
	})
	if len(out.Items) > 20 {
		out.Items = out.Items[:20]
	}
	return out, nil
}

// FailedAttempts reads the last twenty failed attempts, including retries.
// It never returns raw envelopes, credentials or partial results on DB failure.
func (d TaskDiagnostics) FailedAttempts(ctx context.Context, taskID string) ([]FailedAttempt, error) {
	report, err := d.FailedAttemptReport(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !report.Complete {
		return nil, ErrDelivery
	}
	return report.Items, nil
}

func (d TaskDiagnostics) failedAttempts(ctx context.Context, taskID string, source storage.LogSource) ([]FailedAttempt, error) {
	r, s := d.Repository, d.Sanitizer
	if s == nil || taskID == "" || len(taskID) > 128 || r.Database == nil {
		return nil, ErrPayload
	}
	connection, err := r.Registry.LogConnection(source)
	if err != nil {
		return nil, ErrDelivery
	}
	tx, err := r.Database.Begin(ctx, connection)
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
	seen := map[string]string{}
	count := 0
	size := 0
	for rows.Next() {
		var body string
		count++
		if count > 20 || rows.Scan(&body) != nil {
			return nil, ErrDelivery
		}
		if len(body) > s.maxBytes || len(body) > (2<<20)-size {
			return nil, ErrLimit
		}
		size += len(body)
		safe, err := s.restore([]byte(body))
		if err != nil {
			return nil, err
		}
		e := safe.Event()
		if old, ok := seen[safe.ID()]; ok {
			if old != safe.body {
				return nil, ErrConflict
			}
			continue
		}
		seen[safe.ID()] = safe.body
		if e.Identity.TaskID != id || e.Kind != events.Attempt || e.Phase != events.Finished || e.Outcome != events.Failed {
			return nil, ErrPayload
		}
		item := FailedAttempt{Diagnostics: e.Diagnostics, TaskID: e.Identity.TaskID, CallID: e.Identity.CallID, AttemptID: e.Identity.AttemptID, Path: e.Metadata.Path, Mode: e.Metadata.Mode, PageSize: e.Metadata.PageSize, StartTime: e.Metadata.StartTime, EndTime: e.Metadata.EndTime, EventID: e.ID, LogSource: source, At: e.OccurredAt, Source: e.Metadata.Source, Page: e.Metadata.Page, Attempt: e.Identity.AttemptNo, Failure: e.Metadata.Failure, HTTPStatus: e.HTTPStatus, BusinessCode: e.BusinessCode, DurationNS: e.DurationNS, Response: e.Response, Authentication: e.Metadata.Authentication}
		// Keep existing API consumers working while the UI adopts diagnostics.
		for _, request := range e.Diagnostics {
			response := request.Response
			if request.Kind == "authentication" {
				item.Authentication = &diagnostic.Authentication{Endpoint: diagnostic.TokenEndpoint, Reason: request.Failure, HTTPStatus: request.HTTPStatus, Response: response}
			} else if request.HTTPStatus != 0 {
				item.Response = &response
			}
		}
		out = append(out, item)
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
