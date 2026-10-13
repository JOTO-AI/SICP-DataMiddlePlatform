package operations

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type TestRequest struct {
	APIID    string `json:"api_id"`
	Version  string `json:"version"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (r *TestRequest) Normalize() error {
	api, ok := LookupAPI(r.APIID)
	if !ok || api.Kind != "source" || r.Version != CatalogVersion {
		return ErrArgument
	}
	if r.Page == 0 {
		r.Page = 1
	}
	if r.PageSize == 0 {
		r.PageSize = 10
	}
	if r.Page < 1 || r.Page > 1000000 || r.PageSize < 1 || r.PageSize > 100 {
		return ErrArgument
	}
	return nil
}

type resultEntry struct {
	subject string
	expires time.Time
	body    json.RawMessage
}
type Results struct {
	mu    sync.Mutex
	items map[string]resultEntry
	bytes int
}

func NewResults() *Results { return &Results{items: map[string]resultEntry{}} }
func (r *Results) prune(now time.Time) {
	for id, v := range r.items {
		if !now.Before(v.expires) {
			r.bytes -= len(v.body)
			delete(r.items, id)
		}
	}
}
func (r *Results) Put(id, subject string, raw []byte, now time.Time) bool {
	if r == nil || len(raw) > 2<<20 || !json.Valid(raw) {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune(now)
	if old, ok := r.items[id]; ok {
		r.bytes -= len(old.body)
		delete(r.items, id)
	}
	for r.bytes+len(raw) > 20<<20 {
		oldest := ""
		var expiry time.Time
		for key, v := range r.items {
			if oldest == "" || v.expires.Before(expiry) {
				oldest = key
				expiry = v.expires
			}
		}
		if oldest == "" {
			return false
		}
		r.bytes -= len(r.items[oldest].body)
		delete(r.items, oldest)
	}
	r.items[id] = resultEntry{subject, now.Add(10 * time.Minute), append([]byte(nil), raw...)}
	r.bytes += len(raw)
	return true
}
func (r *Results) Get(id, subject string, now time.Time) (json.RawMessage, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune(now)
	v, ok := r.items[id]
	if !ok || v.subject != subject {
		return nil, false
	}
	return append(json.RawMessage(nil), v.body...), true
}

type TestRunner struct {
	Repository Repository
	Collector  *ingest.Coordinator
	Recorder   *events.Recorder
	Results    *Results
	Owner      string
}

func (r *TestRunner) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.Pump(ctx)
		}
	}
}
func (r *TestRunner) Pump(ctx context.Context) error {
	if r == nil || r.Collector == nil || r.Recorder == nil || r.Results == nil {
		return ErrUnavailable
	}
	j, err := r.Repository.Claim(ctx, "source_test", r.Owner, time.Now().UTC())
	if err != nil || j == nil {
		return err
	}
	var request TestRequest
	if json.Unmarshal(j.Request, &request) != nil || request.Normalize() != nil {
		return r.Repository.Finish(ctx, *j, "failed", map[string]any{"reason": "invalid_saved_request"}, time.Now().UTC())
	}
	if e := r.Repository.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error { return CheckSourceTest(ctx, tx) }); e != nil {
		return r.Repository.Finish(ctx, *j, "failed", map[string]any{"reason": "source_busy_or_unavailable"}, time.Now().UTC())
	}
	exec, cancel := context.WithDeadline(ctx, j.Deadline.Add(-5*time.Second))
	defer cancel()
	exec = events.WithRecorder(exec, r.Recorder, events.Identity{Purpose: events.TestPurpose, TestRunID: j.ID})
	budget, err := ingest.NewTaskBudget(j.Deadline, 1)
	if err != nil {
		return err
	}
	result, fetchErr := r.Collector.FetchPage(exec, ingest.PageRequest{Table: request.APIID, Mode: ingest.Full, Page: request.Page, PageSize: request.PageSize}, budget)
	status := "succeeded"
	summary := map[string]any{"result_available": false, "target_status": "unknown", "attempts": len(result.Attempts)}
	if len(result.Attempts) > 0 {
		last := result.Attempts[len(result.Attempts)-1]
		summary["http_status"] = last.Attempt.HTTPStatus
		summary["business_code"] = last.Attempt.BusinessCode
		if last.Attempt.HTTPStatus != nil {
			summary["target_status"] = "responded"
		}
		if last.Failure != "" {
			summary["failure"] = last.Failure
		}
	}
	var raw []byte
	if fetchErr != nil || exec.Err() != nil {
		status = "failed"
	} else {
		list := make([]map[string]json.RawMessage, 0, len(result.Page.Records))
		for _, rec := range result.Page.Records {
			fields := map[string]json.RawMessage{}
			for k, v := range rec.Fields {
				fields[k] = v.Raw
			}
			list = append(list, fields)
		}
		raw, err = json.Marshal(map[string]any{"total": result.Page.Total, "list": list})
		if err != nil || len(raw) > (2<<20)-(64<<10) {
			status = "failed"
			summary["failure"] = "response_limit"
		} else {
			summary["target_status"] = "succeeded"
			summary["record_count"] = len(list)
		}
	}
	if result.ObservationError != nil {
		summary["logs_unavailable"] = true
	}
	finishDeadline := time.Now().Add(5 * time.Second)
	if j.Deadline.Before(finishDeadline) {
		finishDeadline = j.Deadline
	}
	finish, stop := context.WithDeadline(context.WithoutCancel(ctx), finishDeadline)
	defer stop()
	if err = r.Repository.Finish(finish, *j, status, summary, time.Now().UTC()); err != nil {
		return err
	}
	if status == "succeeded" {
		r.Results.Put(j.ID, j.Subject, raw, time.Now().UTC())
	}
	return nil
}
func CheckSourceTest(ctx context.Context, tx storage.Transaction) error {
	var busy bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM control.tasks WHERE status NOT IN ('failed','success_published','success_unchanged')) OR EXISTS(SELECT 1 FROM control.schedule WHERE singleton AND cooldown_until>clock_timestamp())`).Scan(&busy) != nil {
		return ErrUnavailable
	}
	if busy {
		return ErrLimit
	}
	return nil
}
