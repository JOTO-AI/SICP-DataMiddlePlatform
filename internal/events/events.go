// Package events defines the shared source/query lifecycle event boundary.
package events

import (
	"context"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
)

type Kind string
type Phase string
type Outcome string

const (
	Request     Kind    = "request"
	Task        Kind    = "task"
	Step        Kind    = "step"
	Call        Kind    = "call"
	Attempt     Kind    = "attempt"
	Audit       Kind    = "audit"
	Started     Phase   = "started"
	Finished    Phase   = "finished"
	Transport   Phase   = "transport"
	Succeeded   Outcome = "succeeded"
	Failed      Outcome = "failed"
	Interrupted Outcome = "interrupted"
)

var ErrEvent = errors.New("invalid lifecycle event")
var ErrRecord = errors.New("lifecycle event recording failed")

// Identity contains service-generated opaque identifiers, never user identity.
type Identity struct {
	RequestID string  `json:"request_id,omitempty"`
	TaskID    string  `json:"task_id,omitempty"`
	StepID    string  `json:"step_id,omitempty"`
	CallID    string  `json:"call_id,omitempty"`
	AttemptID string  `json:"attempt_id,omitempty"`
	AttemptNo int     `json:"attempt_no,omitempty"`
	Instance  string  `json:"instance,omitempty"`
	Purpose   Purpose `json:"purpose,omitempty"`
	TestRunID string  `json:"test_run_id,omitempty"`
}
type Event struct {
	ID           string               `json:"event_id"`
	EntityID     string               `json:"entity_id"`
	Kind         Kind                 `json:"kind"`
	Phase        Phase                `json:"phase"`
	Identity     Identity             `json:"identity"`
	OccurredAt   time.Time            `json:"occurred_at"`
	Outcome      Outcome              `json:"outcome,omitempty"`
	HTTPStatus   *int                 `json:"http_status,omitempty"`
	BusinessCode *int64               `json:"business_code,omitempty"`
	DurationNS   *int64               `json:"duration_ns,omitempty"`
	Metadata     Metadata             `json:"metadata"`
	Response     *diagnostic.Response `json:"response,omitempty"`
	Audit        *AuditIdentity       `json:"audit,omitempty"`
}

func (Event) String() string        { return "[lifecycle event redacted]" }
func (e Event) GoString() string    { return e.String() }
func (Identity) String() string     { return "[lifecycle identity redacted]" }
func (i Identity) GoString() string { return i.String() }

type Sink interface {
	// Record must honor cancellation. It persists locally; it never waits for PG.
	Record(context.Context, Event) error
}
type SinkFunc func(context.Context, Event) error

func (f SinkFunc) Record(ctx context.Context, e Event) error { return f(ctx, e) }

type Recorder struct {
	mu       sync.Mutex
	prefix   string
	sequence uint64
	clock    ports.Clock
	sink     Sink
	budget   time.Duration
}

func New(clock ports.Clock, random ports.Random, sink Sink, budget time.Duration) (*Recorder, error) {
	if clock == nil || random == nil || sink == nil || budget <= 0 || budget > time.Minute {
		return nil, ErrEvent
	}
	var seed [16]byte
	if n, err := random.Read(seed[:]); err != nil || n != len(seed) {
		return nil, ErrEvent
	}
	return &Recorder{clock: clock, sink: sink, budget: budget, prefix: hex.EncodeToString(seed[:])}, nil
}
func (r *Recorder) newID() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sequence == math.MaxUint64 {
		return "", ErrEvent
	}
	r.sequence++
	return r.prefix + "_" + strconv.FormatUint(r.sequence, 16), nil
}
func validID(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func (i Identity) valid() bool {
	return validID(i.RequestID) && validID(i.TaskID) && validID(i.StepID) && validID(i.CallID) && validID(i.AttemptID) && (i.Instance == "" || i.Instance == "M" || i.Instance == "S") && i.AttemptNo >= 0 && i.validPurpose()
}
func (e Event) Validate() error {
	if e.Metadata.Authentication != nil && (e.Phase != Finished || e.Outcome != Failed || (e.Kind != Attempt && e.Kind != Call)) {
		return ErrEvent
	}
	if (e.Kind == Audit) != (e.Audit != nil) || e.Audit != nil && (!e.Audit.valid() || e.Phase != Finished) {
		return ErrEvent
	}
	if e.Metadata.Build != nil && (e.Kind != Step || e.Identity.TaskID == "" || e.Identity.StepID != e.EntityID || e.Phase == Transport) {
		return ErrEvent
	}
	if e.Response != nil && (!e.Response.Valid() || e.Phase != Finished || e.Outcome != Failed || (e.Kind != Attempt && e.Kind != Call)) {
		return ErrEvent
	}
	if e.ID == "" || e.EntityID == "" || !validID(e.ID) || !validID(e.EntityID) || !e.Identity.valid() || !e.Metadata.valid() || e.OccurredAt.IsZero() {
		return ErrEvent
	}
	switch e.Kind {
	case Request, Task, Step, Call, Attempt, Audit:
	default:
		return ErrEvent
	}
	switch e.Phase {
	case Started:
		if e.Outcome != "" || e.HTTPStatus != nil || e.BusinessCode != nil || e.DurationNS != nil {
			return ErrEvent
		}
	case Finished:
		if e.Outcome != Succeeded && e.Outcome != Failed && e.Outcome != Interrupted {
			return ErrEvent
		}
	case Transport:
		if e.Kind != Request || (e.Outcome != Succeeded && e.Outcome != Interrupted) {
			return ErrEvent
		}
	default:
		return ErrEvent
	}
	if e.HTTPStatus != nil && (*e.HTTPStatus < 100 || *e.HTTPStatus > 599) {
		return ErrEvent
	}
	if e.DurationNS != nil && *e.DurationNS < 0 {
		return ErrEvent
	}
	return nil
}

type binding struct {
	recorder *Recorder
	identity Identity
}
type contextKey struct{}

func WithRecorder(ctx context.Context, r *Recorder, i Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, binding{r, i})
}

// Begin is optional only when no recorder has been installed in the context.
func Begin(ctx context.Context, k Kind, instance string, attempt int) (context.Context, *Span, error) {
	b, _ := ctx.Value(contextKey{}).(binding)
	if b.recorder == nil {
		return ctx, nil, nil
	}
	return b.recorder.Begin(ctx, k, b.identity, instance, attempt)
}

type Span struct {
	mu                              sync.Mutex
	recorder                        *Recorder
	base                            Event
	started                         time.Time
	finished, transported           bool
	pendingFinish, pendingTransport *Event
}

func (r *Recorder) Begin(ctx context.Context, k Kind, parent Identity, instance string, attempt int) (context.Context, *Span, error) {
	if r == nil || !parent.valid() {
		return ctx, nil, ErrEvent
	}
	id, err := r.newID()
	if err != nil {
		return ctx, nil, err
	}
	i := parent
	switch k {
	case Request:
		i = Identity{RequestID: id}
		// The HTTP service may supply its already generated trusted request ID.
		// Incoming client headers must never be passed as this internal identity.
		if parent.RequestID != "" {
			i.RequestID = parent.RequestID
		}
	case Task:
		i.TaskID = id
		i.StepID = ""
		i.CallID = ""
		i.AttemptID = ""
		i.AttemptNo = 0
	case Step:
		if i.TaskID == "" || (instance != "M" && instance != "S") {
			return ctx, nil, ErrEvent
		}
		i.StepID = id
		i.Instance = instance
		i.CallID = ""
		i.AttemptID = ""
		i.AttemptNo = 0
	case Call:
		if i.Purpose == "" {
			i.Purpose = SyncPurpose
		}
		i.CallID = id
		i.AttemptID = ""
		i.AttemptNo = 0
	case Attempt:
		if i.Purpose == "" {
			i.Purpose = SyncPurpose
		}
		if i.CallID == "" || attempt < 1 {
			return ctx, nil, ErrEvent
		}
		i.AttemptID = id
		i.AttemptNo = attempt
	default:
		return ctx, nil, ErrEvent
	}
	now := r.clock.Now()
	e := Event{ID: id + "_start", EntityID: id, Kind: k, Phase: Started, Identity: i, OccurredAt: now.UTC()}
	e.Metadata, _ = ctx.Value(metadataKey{}).(Metadata)
	if err := e.Validate(); err != nil {
		return ctx, nil, err
	}
	recordCtx, cancel := context.WithTimeout(ctx, r.budget)
	defer cancel()
	if err := r.sink.Record(recordCtx, e); err != nil || recordCtx.Err() != nil {
		return ctx, nil, ErrRecord
	}
	s := &Span{recorder: r, base: e, started: now}
	return WithRecorder(ctx, r, i), s, nil
}
func (s *Span) Identity() Identity {
	if s == nil {
		return Identity{}
	}
	return s.base.Identity
}

// FinishedAt exposes the frozen terminal fact even if its log acknowledgement
// failed. A notification must not invent a new occurrence time on replay.
func (s *Span) FinishedAt() (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingFinish == nil {
		return time.Time{}, false
	}
	return s.pendingFinish.OccurredAt, true
}

// Finish records business completion; Transport records client delivery separately.
func (s *Span) Finish(ctx context.Context, outcome Outcome, status *int, code *int64) error {
	return s.end(ctx, Finished, outcome, status, code, "", nil, nil)
}
func (s *Span) FinishFailure(ctx context.Context, outcome Outcome, status *int, code *int64, failure string) error {
	return s.end(ctx, Finished, outcome, status, code, failure, nil, nil)
}
func (s *Span) FinishResponse(ctx context.Context, outcome Outcome, status *int, code *int64, failure string, response *diagnostic.Response) error {
	return s.end(ctx, Finished, outcome, status, code, failure, response, nil)
}
func (s *Span) FinishSource(ctx context.Context, outcome Outcome, status *int, code *int64, failure string, response *diagnostic.Response, authentication *diagnostic.Authentication) error {
	return s.end(ctx, Finished, outcome, status, code, failure, response, authentication)
}
func (s *Span) Transport(ctx context.Context, outcome Outcome, status *int) error {
	return s.end(ctx, Transport, outcome, status, nil, "", nil, nil)
}
func (s *Span) end(ctx context.Context, phase Phase, outcome Outcome, status *int, code *int64, failure string, response *diagnostic.Response, authentication *diagnostic.Authentication) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if phase == Finished && s.finished || phase == Transport && s.transported {
		return ErrEvent
	}
	pending := &s.pendingFinish
	if phase == Transport {
		pending = &s.pendingTransport
	}
	var e Event
	if *pending != nil {
		old := **pending
		if old.Outcome != outcome || old.Metadata.Failure != failure || !sameValue(old.HTTPStatus, status) || !sameValue(old.BusinessCode, code) || !sameValue(old.Response, response) || !sameValue(old.Metadata.Authentication, authentication) {
			return ErrEvent
		}
		e = old
	} else {
		now := s.recorder.clock.Now()
		elapsed := now.Sub(s.started).Nanoseconds()
		if elapsed < 0 {
			return ErrEvent
		}
		e = s.base
		e.ID = e.EntityID + "_" + string(phase)
		e.Phase = phase
		e.OccurredAt = now.UTC()
		e.Outcome = outcome
		e.Metadata.Failure = failure
		if authentication != nil {
			copy := *authentication
			e.Metadata.Authentication = &copy
		}
		if response != nil {
			copy := *response
			e.Response = &copy
		}
		e.DurationNS = &elapsed
		if status != nil {
			v := *status
			e.HTTPStatus = &v
		}
		if code != nil {
			v := *code
			e.BusinessCode = &v
		}
		if err := e.Validate(); err != nil {
			return err
		}
		*pending = &e
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.recorder.budget)
	defer cancel()
	if err := s.recorder.sink.Record(recordCtx, e); err != nil || recordCtx.Err() != nil {
		return ErrRecord
	}
	if phase == Finished {
		s.finished = true
	} else {
		s.transported = true
	}
	return nil
}
func sameValue[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
