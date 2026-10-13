package notification

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

var ErrDelivery = errors.New("notification delivery failed or outcome unknown")
var ErrConfiguration = errors.New("invalid notification delivery configuration")

// Unknown means a crash, cancellation, or lost response may have hidden channel
// acceptance. Rejected is used only when the channel positively reports it.
type Attempt struct {
	Number     int        `json:"number"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Result     string     `json:"result"`
}
type Delivery struct {
	Attempts    []Attempt  `json:"attempts"`
	Exhausted   bool       `json:"exhausted"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
}
type Mail struct{ ID, From, To, Subject, Body string }

func (Mail) String() string     { return "[notification mail redacted]" }
func (m Mail) GoString() string { return m.String() }

type Receipt struct {
	Accepted bool
	Rejected bool
	// Temporary proves no DATA submission was accepted; unknown outcomes must
	// leave this false so they are never retried by the management sender.
	Temporary bool
	// DeliveredAt must be backed by explicit provider delivery confirmation.
	// A successful SMTP submission alone MUST leave this nil.
	DeliveredAt *time.Time
}

// Channel must honor context and reuse Mail.ID as its provider idempotency key
// where supported. Ordinary SMTP does not guarantee duplicate-free delivery.
type Channel interface {
	Send(context.Context, Mail) (Receipt, error)
}
type DeliveryStore interface {
	DueNotifications(context.Context, time.Time, int, int) ([]control.Notification, error)
	AdvanceNotification(context.Context, control.Notification, control.Notification) (bool, error)
}
type DeliveryConfig struct {
	TrustedFields                                []string
	From, To                                     string
	MaxAttempts, Batch                           int
	RetryDelay, SendTimeout, StoreTimeout, Lease time.Duration
}

func (DeliveryConfig) String() string     { return "[notification configuration redacted]" }
func (c DeliveryConfig) GoString() string { return c.String() }

type Sender struct {
	store     DeliveryStore
	channel   Channel
	clock     ports.Clock
	sanitizer *observability.Sanitizer
	config    DeliveryConfig
}

func (Sender) String() string     { return "[notification sender redacted]" }
func (s Sender) GoString() string { return s.String() }
func NewSender(store DeliveryStore, channel Channel, clock ports.Clock, sanitizer *observability.Sanitizer, c DeliveryConfig) (*Sender, error) {
	address := func(v string) bool {
		if len(v) > 254 || strings.ContainsAny(v, "\r\n") {
			return false
		}
		a, e := mail.ParseAddress(v)
		return e == nil && a.Address == v && strings.Contains(v, "@")
	}
	if store == nil || channel == nil || clock == nil || sanitizer == nil || !address(c.From) || !address(c.To) || c.MaxAttempts < 1 || c.MaxAttempts > 16 || c.Batch < 1 || c.Batch > 100 || c.RetryDelay < time.Microsecond || c.RetryDelay > 24*time.Hour || c.SendTimeout < time.Millisecond || c.SendTimeout > time.Minute || c.StoreTimeout < time.Millisecond || c.StoreTimeout > time.Minute || c.Lease < c.SendTimeout+2*c.StoreTimeout || c.Lease > 5*time.Minute {
		return nil, ErrConfiguration
	}
	if len(c.TrustedFields) > 128 {
		return nil, ErrConfiguration
	}
	for _, field := range c.TrustedFields {
		if name, _ := sanitizer.SchemaField(field, field); name == "" {
			return nil, ErrConfiguration
		}
	}
	c.TrustedFields = append([]string(nil), c.TrustedFields...)
	return &Sender{store, channel, clock, sanitizer, c}, nil
}

// Pump makes one bounded pass. The worker supplies the scheduling loop. A new
// Sender with a larger total MaxAttempts explicitly resumes exhausted notices,
// preserving all prior attempts; 16 is a hard bound requiring operator action.
func (s *Sender) Pump(ctx context.Context) (int, error) {
	if s == nil {
		return 0, ErrConfiguration
	}
	read, cancel := context.WithTimeout(ctx, s.config.StoreTimeout)
	rows, err := s.store.DueNotifications(read, s.clock.Now(), s.config.Batch, s.config.MaxAttempts)
	cancel()
	if err != nil {
		return 0, ErrOutbox
	}
	if len(rows) > s.config.Batch {
		return 0, ErrOutbox
	}
	count := 0
	var combined error
	for _, n := range rows {
		if ctx.Err() != nil {
			return count, errors.Join(combined, ErrDelivery)
		}
		sent, e := s.send(ctx, n)
		if sent {
			count++
		}
		combined = errors.Join(combined, e)
	}
	return count, combined
}
func (s *Sender) send(ctx context.Context, n control.Notification) (bool, error) {
	p, err := s.decode(n)
	if err != nil {
		return false, err
	}
	now := s.clock.Now().UTC()
	if n.Attempts >= s.config.MaxAttempts || (n.Status != "pending" && n.Status != "sending") || (n.NextAttemptAt != nil && now.Before(*n.NextAttemptAt)) {
		return false, nil
	}
	if p.Delivery == nil {
		p.Delivery = &Delivery{}
	}
	// An expired unknown claim remains in history; never rewrite it as rejection.
	p.Delivery.Attempts = append(p.Delivery.Attempts, Attempt{Number: n.Attempts + 1, StartedAt: now, Result: "unknown"})
	p.Delivery.Exhausted = n.Attempts+1 >= s.config.MaxAttempts
	claimed := n
	claimed.Status = "sending"
	claimed.Attempts++
	lease := now.Add(s.config.Lease).Truncate(time.Microsecond)
	claimed.NextAttemptAt = &lease
	claimed.Payload, err = encode(p)
	if err != nil {
		return false, err
	}
	op, cancel := context.WithTimeout(ctx, s.config.StoreTimeout)
	ok, err := s.store.AdvanceNotification(op, n, claimed)
	cancel()
	if err != nil {
		return false, ErrOutbox
	}
	if !ok {
		return false, nil
	}
	// Freeze the mail body to the immutable notice, excluding attempt metadata.
	mailNotice := p.Notice
	mailNotice.Fields = append([]Field(nil), p.Notice.Fields...)
	for i := range mailNotice.Fields {
		f := &mailNotice.Fields[i]
		name, reference := s.sanitizer.SchemaField(f.Name, s.config.TrustedFields...)
		if name == "" || reference != f.Reference {
			f.Name = ""
		}
	}
	body, _ := json.Marshal(mailNotice)
	call, stop := context.WithTimeout(ctx, s.config.SendTimeout)
	receipt, sendErr := s.channel.Send(call, Mail{ID: n.FailureKey, From: s.config.From, To: s.config.To, Subject: "Source schema validation failed", Body: string(body)})
	callErr := call.Err()
	stop()
	finished := s.clock.Now().UTC()
	a := &p.Delivery.Attempts[len(p.Delivery.Attempts)-1]
	a.FinishedAt = &finished
	next := claimed
	next.Status = "pending"
	retry := finished.Add(s.config.RetryDelay)
	next.NextAttemptAt = &retry
	accepted := sendErr == nil && callErr == nil && receipt.Accepted && !receipt.Rejected
	if accepted {
		a.Result = "accepted"
		next.Status = "accepted"
		next.SentAt = &finished
		next.NextAttemptAt = nil
		p.Delivery.Exhausted = false
		if receipt.DeliveredAt != nil && !receipt.DeliveredAt.IsZero() {
			t := receipt.DeliveredAt.UTC()
			p.Delivery.DeliveredAt = &t
			next.Status = "delivered"
			a.Result = "delivered"
		}
	} else if sendErr == nil && callErr == nil && receipt.Rejected && !receipt.Accepted && receipt.DeliveredAt == nil {
		a.Result = "rejected"
	}
	next.Payload, err = encode(p)
	if err != nil {
		return false, err
	}
	// Cancellation of acquisition must not erase an already observed send result;
	// this independent persistence attempt has a strict finite budget.
	save, done := context.WithTimeout(context.WithoutCancel(ctx), s.config.StoreTimeout)
	ok, err = s.store.AdvanceNotification(save, claimed, next)
	done()
	if err != nil || !ok {
		return false, ErrOutbox
	}
	if !accepted {
		return false, ErrDelivery
	}
	return true, nil
}
func encode(p Payload) (control.Document, error) {
	b, e := json.Marshal(p)
	if e != nil || len(b) > 16384 {
		return "", ErrNotice
	}
	return control.Document(b), nil
}
func (s *Sender) decode(row control.Notification) (Payload, error) {
	var p Payload
	if len(row.Payload) > 16384 {
		return p, ErrNotice
	}
	decoder := json.NewDecoder(strings.NewReader(string(row.Payload)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil {
		return p, ErrNotice
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return p, ErrNotice
	}
	n := p.Notice
	hash := func(v string) bool {
		b, e := hex.DecodeString(v)
		return e == nil && len(b) == 32 && strings.ToLower(v) == v
	}
	if !hash(n.FailureID) || !hash(n.TaskID) || !hash(n.CallID) || !hash(n.Fingerprint) || row.ID != n.FailureID || row.FailureKey != n.FailureID || s.sanitizer.Correlation(row.TaskID) != n.TaskID || n.OccurredAt.IsZero() || n.Page < 1 || n.PageSize < 1 || n.Attempts < 1 || n.Attempts > 100 || n.UnknownCount < 1 || len(n.Fields) != min(n.UnknownCount, 64) || n.Truncated != (n.UnknownCount > 64) || !failureKind(n.Reason) || (n.Mode != ingest.Full && n.Mode != ingest.ForceFull && n.Mode != ingest.Incremental) {
		return p, ErrNotice
	}
	if n.Environment != "test" && n.Environment != "development" && n.Environment != "staging" && n.Environment != "production" {
		return p, ErrNotice
	}
	known := false
	for _, source := range ingest.Catalog() {
		if source.Name == n.Source && source.Path == n.Path {
			known = true
		}
	}
	if !known {
		return p, ErrNotice
	}
	for _, f := range p.Notice.Fields {
		if !hash(f.Reference) {
			return p, ErrNotice
		}
	}
	if row.Attempts < 0 || row.Attempts > 16 {
		return p, ErrNotice
	}
	if p.Delivery == nil {
		if row.Attempts != 0 {
			return p, ErrNotice
		}
	} else {
		if len(p.Delivery.Attempts) != row.Attempts {
			return p, ErrNotice
		}
		for i, a := range p.Delivery.Attempts {
			if a.Number != i+1 || a.StartedAt.IsZero() || (a.Result != "unknown" && a.Result != "rejected" && a.Result != "accepted" && a.Result != "delivered") || (a.Result != "unknown" && a.FinishedAt == nil) {
				return p, ErrNotice
			}
		}
	}
	return p, nil
}
