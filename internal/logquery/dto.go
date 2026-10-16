package logquery

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

const (
	MaxRequestBytes  = 64 << 10
	MaxEventBytes    = 256 << 10
	MaxResponseBytes = 2 << 20
)

// DecodeRequest bounds the complete body before JSON parsing. The HTTP adapter
// must additionally enforce its read deadline and authenticate before decoding.
func DecodeRequest(body io.Reader, target any) error {
	if body == nil || target == nil {
		return ErrArgument
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxRequestBytes+1))
	if err != nil {
		return ErrArgument
	}
	if len(data) > MaxRequestBytes {
		return ErrTooLarge
	}
	if data = bytes.TrimSpace(data); len(data) == 0 || data[0] != '{' {
		return ErrArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrArgument
	}
	return nil
}

// Purpose and audit fields are contract declarations for P28 event validation,
// sanitation and schema work; declaring them does not enable the old writer.
type Purpose string

const (
	SyncPurpose Purpose = "sync"
	TestPurpose Purpose = "test"
)

type TestIdentity struct {
	Purpose   Purpose `json:"purpose"`
	TestRunID string  `json:"test_run_id"`
}

func (i TestIdentity) Validate() error {
	if i.Purpose == SyncPurpose && i.TestRunID == "" {
		return nil
	}
	if i.Purpose == TestPurpose && i.TestRunID != "" && validCorrelation(i.TestRunID) {
		return nil
	}
	return ErrArgument
}

// Audit contains only trusted actor correlation and server-owned action codes.
// No request bodies, email addresses, headers or arbitrary text are allowed.
type AuditIdentity struct {
	ActorID  string `json:"actor_id"`
	Action   string `json:"action"`
	ObjectID string `json:"object_id"`
}

func (a AuditIdentity) Validate() error {
	if a.ActorID == "" || !validCorrelation(a.ActorID) || !validCorrelation(a.ObjectID) {
		return ErrArgument
	}
	switch a.Action {
	case "source_test", "notification_settings", "notification_test", "retention_settings", "cleanup":
		return nil
	default:
		return ErrArgument
	}
}

// EventItem is a whitelisted summary. Optional facts remain null, including
// unknown duration/status and missing relationships. All IDs are correlations.
type EventItem struct {
	Context      *EventContext       `json:"context,omitempty"`
	Source       Source              `json:"source"`
	Epoch        string              `json:"epoch"`
	EventID      string              `json:"event_id"`
	Sequence     int64               `json:"sequence,string"`
	Kind         events.Kind         `json:"kind"`
	Phase        events.Phase        `json:"phase"`
	OccurredAt   *time.Time          `json:"occurred_at"`
	ReceivedAt   *time.Time          `json:"received_at"`
	Outcome      *events.Outcome     `json:"outcome"`
	Operation    *contract.Operation `json:"operation"`
	RequestID    *string             `json:"request_id"`
	TaskID       *string             `json:"task_id"`
	StepID       *string             `json:"step_id"`
	CallID       *string             `json:"call_id"`
	AttemptID    *string             `json:"attempt_id"`
	AttemptNo    *int                `json:"attempt_no"`
	HTTPStatus   *int                `json:"http_status"`
	BusinessCode *int64              `json:"business_code"`
	DurationNS   *int64              `json:"duration_ns,string"`
	Test         *TestIdentity       `json:"test"`
	Audit        *AuditIdentity      `json:"audit"`
}

// EventContext names measured work using only already-sanitized, typed facts.
// It excludes response bodies, diagnostics, raw identities and build metrics.
type EventContext struct {
	Stage     string             `json:"stage,omitempty"`
	Table     string             `json:"table,omitempty"`
	Instance  string             `json:"instance,omitempty"`
	Slot      *int               `json:"slot,omitempty"`
	Attempt   int                `json:"attempt,omitempty"`
	Page      int                `json:"page,omitempty"`
	Mode      string             `json:"mode,omitempty"`
	Operation contract.Operation `json:"operation,omitempty"`
}

type Redaction struct {
	Redacted      bool  `json:"redacted"`
	Truncated     bool  `json:"truncated"`
	OriginalBytes int64 `json:"original_bytes,string"`
	FieldCount    int   `json:"field_count"`
}

// Detail only carries typed, sanitized fields. P29 must validate restored events
// and apply MaxEventBytes before constructing this DTO, never expose raw JSON.
type Detail struct {
	EventItem
	Diagnostics []diagnostic.Request `json:"diagnostics,omitempty"`
	EntityID    string               `json:"entity_id"`
	Metadata    events.Metadata      `json:"metadata"`
	Response    *diagnostic.Response `json:"response"`
	Redaction   Redaction            `json:"redaction"`
}

type Page struct {
	Items           []EventItem `json:"items"`
	Filter          Filter      `json:"filter"`
	Source          Source      `json:"source"`
	Epoch           string      `json:"epoch"`
	HighWater       int64       `json:"high_water,string"`
	RetainedThrough int64       `json:"retained_through,string"`
	NextCursor      *string     `json:"next_cursor"`
	StreamCursor    *string     `json:"stream_cursor"`
	ExpiresAt       *time.Time  `json:"expires_at"`
}

func (p Page) MarshalJSON() ([]byte, error) {
	type plain Page
	if p.Items == nil {
		p.Items = []EventItem{}
	}
	return json.Marshal(plain(p))
}
