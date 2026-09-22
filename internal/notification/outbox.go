// Package notification persists final unknown-field failures independently of logs.
package notification

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

var ErrNotice = errors.New("invalid final-failure notification")
var ErrOutbox = errors.New("notification outbox persistence failed")
var ErrConflict = errors.New("notification failure identity conflict")

type Field struct {
	Name      string `json:"name,omitempty"`
	Reference string `json:"reference"`
}
type Notice struct {
	Environment  string             `json:"environment"`
	FailureID    string             `json:"failure_id"`
	TaskID       string             `json:"task_id"`
	CallID       string             `json:"call_id"`
	Fingerprint  string             `json:"fingerprint"`
	Source       string             `json:"source"`
	Path         string             `json:"path"`
	Mode         ingest.Mode        `json:"mode"`
	Page         int                `json:"page"`
	PageSize     int                `json:"page_size"`
	Attempts     int                `json:"attempts"`
	OccurredAt   time.Time          `json:"occurred_at"`
	Reason       ingest.FailureKind `json:"reason"`
	UnknownCount int                `json:"unknown_count"`
	Fields       []Field            `json:"fields"`
	Truncated    bool               `json:"truncated"`
}
type Payload struct {
	Notice   Notice    `json:"notice"`
	Delivery *Delivery `json:"delivery,omitempty"`
}
type Outbox interface {
	EnqueueNotification(context.Context, control.Notification) (control.Notification, bool, error)
}
type Consumer struct {
	outbox        Outbox
	sanitizer     *observability.Sanitizer
	environment   string
	visibleFields []string
}

func NewConsumer(outbox Outbox, s *observability.Sanitizer, environment string, visibleFields ...string) (*Consumer, error) {
	if outbox == nil || s == nil || len(visibleFields) > 128 || (environment != "test" && environment != "development" && environment != "staging" && environment != "production") {
		return nil, ErrNotice
	}
	for _, field := range visibleFields {
		if name, _ := s.SchemaField(field, field); name == "" {
			return nil, ErrNotice
		}
	}
	return &Consumer{outbox: outbox, sanitizer: s, environment: environment, visibleFields: append([]string(nil), visibleFields...)}, nil
}
func (c Consumer) String() string   { return "[notification consumer redacted]" }
func (c Consumer) GoString() string { return c.String() }

// Consume is called with the actual FetchPage/Collect FinalFailure. Successful
// retries have no final event. Persistence errors remain visible to the worker.
func (c *Consumer) Consume(ctx context.Context, e *ingest.FailureEvent) (control.Notification, bool, error) {
	if e == nil {
		return control.Notification{}, false, nil
	}
	if c == nil || !e.IsFinal() || e.TaskID == "" || e.CallID == "" || len(e.TaskID) > 128 || len(e.CallID) > 128 || e.OccurredAt.IsZero() || e.Attempts < 1 || e.Attempts > 100 || len(e.UnknownFields) == 0 {
		return control.Notification{}, false, ErrNotice
	}
	known := false
	for _, source := range ingest.Catalog() {
		if source.Name == e.Source && source.Path == e.Path {
			known = true
			break
		}
	}
	if !known || e.Page < 1 || e.PageSize < 1 || (e.Mode != ingest.Full && e.Mode != ingest.ForceFull && e.Mode != ingest.Incremental) || !failureKind(e.Reason) {
		return control.Notification{}, false, ErrNotice
	}
	fields := make([]Field, 0, min(len(e.UnknownFields), 64))
	last := ""
	for i, raw := range e.UnknownFields {
		if i > 0 && raw <= last {
			return control.Notification{}, false, ErrNotice
		}
		last = raw
		if i < 64 {
			name, ref := c.sanitizer.SchemaField(raw, c.visibleFields...)
			fields = append(fields, Field{name, ref})
		}
	}
	id := c.sanitizer.Fingerprint("failure", e.TaskID, e.CallID)
	n := Notice{Environment: c.environment, FailureID: id, TaskID: c.sanitizer.Correlation(e.TaskID), CallID: c.sanitizer.Correlation(e.CallID), Fingerprint: c.sanitizer.Fingerprint(e.TaskID, e.CallID, e.Source, e.Path, string(e.Mode), strconv.Itoa(e.Page), strconv.Itoa(e.PageSize), strconv.Itoa(e.Attempts), e.OccurredAt.UTC().Format(time.RFC3339Nano), string(e.Reason), c.sanitizer.Fingerprint(e.UnknownFields...)), Source: e.Source, Path: e.Path, Mode: e.Mode, Page: e.Page, PageSize: e.PageSize, Attempts: e.Attempts, OccurredAt: e.OccurredAt.UTC(), Reason: e.Reason, UnknownCount: len(e.UnknownFields), Fields: fields, Truncated: len(e.UnknownFields) > 64}
	body, err := json.Marshal(Payload{Notice: n})
	if err != nil || len(body) > 16384 || strings.TrimSpace(e.TaskID) == "" {
		return control.Notification{}, false, ErrNotice
	}
	row, created, err := c.outbox.EnqueueNotification(ctx, control.Notification{ID: id, FailureKey: id, TaskID: e.TaskID, Payload: control.Document(body), Status: "pending", CreatedAt: n.OccurredAt})
	if errors.Is(err, control.ErrIdempotency) {
		return control.Notification{}, false, ErrConflict
	}
	if err != nil {
		return control.Notification{}, false, ErrOutbox
	}
	return row, created, nil
}
func failureKind(k ingest.FailureKind) bool {
	switch k {
	case ingest.ConfigurationFailure, ingest.TransportFailure, ingest.HTTPFailure, ingest.BusinessFailure, ingest.StructureFailure, ingest.ResponseLimit, ingest.CancelledFailure, ingest.RecordFailure, ingest.RetryWaitFailure, ingest.ObservationFailure, ingest.BudgetFailure, ingest.RequestTimeout, ingest.CooldownFailure, ingest.CooldownStorageFailure:
		return true
	}
	return false
}
