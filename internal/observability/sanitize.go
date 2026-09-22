// Package observability accepts only sanitized, bounded event payloads.
package observability

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
)

var ErrPayload = errors.New("invalid sanitized event payload")
var ErrLimit = errors.New("event payload limit exceeded")
var ErrBuffer = errors.New("event buffer unavailable")
var ErrRecursive = errors.New("recursive event recording rejected")

type recordingKey struct{}

// Details is transient input. Arbitrary text, field names and values are all
// untrusted: they are replaced, not copied into diagnostics or error strings.
type Details struct {
	Message string
	Fields  map[string]string
}

func (Details) String() string     { return "[untrusted event details redacted]" }
func (d Details) GoString() string { return d.String() }

type detailSummary struct {
	Redacted      bool  `json:"redacted"`
	OriginalBytes int64 `json:"original_bytes"`
	FieldCount    int   `json:"field_count"`
	Truncated     bool  `json:"truncated"`
}
type envelope struct {
	Event   events.Event  `json:"event"`
	Details detailSummary `json:"details"`
}

// SafeEvent's zero value is invalid. There is no public payload constructor.
// Bytes always returns a copy; all durable/diagnostic/notification outputs use
// this same representation rather than accepting arbitrary text.
type SafeEvent struct {
	body string
	id   string
}

func (s SafeEvent) Valid() bool      { return s.body != "" && s.id != "" }
func (s SafeEvent) ID() string       { return s.id }
func (s SafeEvent) Bytes() []byte    { return []byte(s.body) }
func (s SafeEvent) String() string   { return "[sanitized event]" }
func (s SafeEvent) GoString() string { return s.String() }
func (s SafeEvent) Event() events.Event {
	var e envelope
	_ = json.Unmarshal([]byte(s.body), &e)
	return e.Event
}

type SafeSink interface {
	Append(context.Context, SafeEvent) error
}
type SafeSinkFunc func(context.Context, SafeEvent) error

func (f SafeSinkFunc) Append(ctx context.Context, e SafeEvent) error { return f(ctx, e) }

type Sanitizer struct {
	maxBytes, detailBytes int
	secrets               []string
	key                   [32]byte
}

func (Sanitizer) String() string     { return "[event sanitizer redacted]" }
func (s Sanitizer) GoString() string { return s.String() }

func NewSanitizer(maxBytes, detailBytes int, key []byte, secrets []string) (*Sanitizer, error) {
	if maxBytes < 512 || maxBytes > 1<<20 || detailBytes < 1 || detailBytes > maxBytes || len(secrets) > 100 || len(key) != 32 || bytes.Equal(key, make([]byte, 32)) {
		return nil, ErrPayload
	}
	copySecrets := append([]string(nil), secrets...)
	for _, secret := range copySecrets {
		if len(secret) < 8 || len(secret) > 4096 {
			return nil, ErrPayload
		}
	}
	s := &Sanitizer{maxBytes: maxBytes, detailBytes: detailBytes, secrets: copySecrets}
	copy(s.key[:], key)
	return s, nil
}

// Correlation maps a fixed-control task ID to its log identity. Configuration
// must retain the same key across restarts and all services sharing these logs.
func (s *Sanitizer) Correlation(raw string) string {
	if s == nil || raw == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}
func (s *Sanitizer) Sanitize(e events.Event, d Details) (SafeEvent, error) {
	if s == nil || e.Validate() != nil {
		return SafeEvent{}, ErrPayload
	}
	size := int64(len(d.Message))
	for key, value := range d.Fields {
		if int64(len(key))+int64(len(value)) > (1<<62)-size {
			return SafeEvent{}, ErrLimit
		}
		size += int64(len(key)) + int64(len(value))
	}
	e.OccurredAt = e.OccurredAt.UTC()
	e.ID = s.Correlation(e.ID)
	e.EntityID = s.Correlation(e.EntityID)
	e.Identity.RequestID = s.Correlation(e.Identity.RequestID)
	e.Identity.TaskID = s.Correlation(e.Identity.TaskID)
	e.Identity.StepID = s.Correlation(e.Identity.StepID)
	e.Identity.CallID = s.Correlation(e.Identity.CallID)
	e.Identity.AttemptID = s.Correlation(e.Identity.AttemptID)
	env := envelope{e, detailSummary{Redacted: size > 0, OriginalBytes: size, FieldCount: len(d.Fields), Truncated: size > int64(s.detailBytes)}}
	body, err := json.Marshal(env)
	if err != nil {
		return SafeEvent{}, ErrPayload
	}
	return s.restore(body)
}
func (s *Sanitizer) restore(body []byte) (SafeEvent, error) {
	if s == nil || len(body) > s.maxBytes {
		return SafeEvent{}, ErrLimit
	}
	var env envelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&env) != nil {
		return SafeEvent{}, ErrPayload
	}
	if decoder.Decode(new(any)) != io.EOF {
		return SafeEvent{}, ErrPayload
	}
	if env.Event.Validate() != nil || env.Details.OriginalBytes < 0 || env.Details.FieldCount < 0 || env.Details.Redacted != (env.Details.OriginalBytes > 0) || env.Details.Truncated != (env.Details.OriginalBytes > int64(s.detailBytes)) {
		return SafeEvent{}, ErrPayload
	}
	if !digestID(env.Event.ID, false) || !digestID(env.Event.EntityID, false) {
		return SafeEvent{}, ErrPayload
	}
	i := env.Event.Identity
	for _, id := range []string{i.RequestID, i.TaskID, i.StepID, i.CallID, i.AttemptID} {
		if !digestID(id, true) {
			return SafeEvent{}, ErrPayload
		}
	}
	// Check metadata too: caller-supplied correlation identifiers must never
	// smuggle configured secrets around the detail redaction boundary.
	canonical, err := json.Marshal(env)
	if err != nil {
		return SafeEvent{}, ErrPayload
	}
	for _, secret := range s.secrets {
		if strings.Contains(string(canonical), secret) {
			return SafeEvent{}, ErrPayload
		}
	}
	if len(canonical) > s.maxBytes {
		return SafeEvent{}, ErrLimit
	}
	return SafeEvent{body: string(canonical), id: env.Event.ID}, nil
}
func digestID(id string, optional bool) bool {
	if id == "" {
		return optional
	}
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type Guard struct {
	sanitizer *Sanitizer
	sink      SafeSink
}

func (Guard) String() string     { return "[event guard redacted]" }
func (g Guard) GoString() string { return g.String() }

func NewGuard(s *Sanitizer, sink SafeSink) (*Guard, error) {
	if s == nil || sink == nil {
		return nil, ErrPayload
	}
	return &Guard{s, sink}, nil
}
func (g *Guard) Record(ctx context.Context, e events.Event) error {
	return g.RecordDetails(ctx, e, Details{})
}
func (g *Guard) RecordDetails(ctx context.Context, e events.Event, d Details) error {
	if g == nil {
		return ErrPayload
	}
	if active, _ := ctx.Value(recordingKey{}).(bool); active {
		return ErrRecursive
	}
	safe, err := g.sanitizer.Sanitize(e, d)
	if err != nil {
		return err
	}
	if err := g.sink.Append(context.WithValue(ctx, recordingKey{}, true), safe); err != nil {
		if errors.Is(err, ErrCapacity) {
			return ErrCapacity
		}
		if errors.Is(err, ErrClosed) {
			return ErrClosed
		}
		return ErrBuffer
	}
	return nil
}
