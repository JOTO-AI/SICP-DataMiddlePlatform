package control_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type mailClock struct{ now time.Time }

func (c *mailClock) Now() time.Time { return c.now }

type mockMail struct {
	calls []notification.Mail
	send  func(notification.Mail) (notification.Receipt, error)
}

func (m *mockMail) Send(ctx context.Context, v notification.Mail) (notification.Receipt, error) {
	if ctx.Err() != nil {
		return notification.Receipt{}, ctx.Err()
	}
	m.calls = append(m.calls, v)
	return m.send(v)
}
func deliveryConfig() notification.DeliveryConfig {
	return notification.DeliveryConfig{From: "worker@example.invalid", To: "ops@example.invalid", TrustedFields: []string{"NEW_COLUMN"}, MaxAttempts: 3, Batch: 10, RetryDelay: time.Second, SendTimeout: time.Second, StoreTimeout: time.Second, Lease: 4 * time.Second}
}
func sender(t *testing.T, r control.Repository, s *observability.Sanitizer, c *mailClock, m *mockMail, config notification.DeliveryConfig) *notification.Sender {
	t.Helper()
	v, e := notification.NewSender(r, m, c, s, config)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func loadNotice(t *testing.T, r control.Repository, id string) (control.Notification, notification.Payload) {
	t.Helper()
	n := control.Notification{ID: id}
	if e := r.Load(context.Background(), &n); e != nil {
		t.Fatal(e)
	}
	var p notification.Payload
	if e := json.Unmarshal([]byte(n.Payload), &p); e != nil {
		t.Fatal(e)
	}
	return n, p
}
func enqueueMail(t *testing.T, c *notification.Consumer) control.Notification {
	t.Helper()
	f, _ := sourceFinal(t, ingest.Full, []string{`{"XQH":"01","NEW_COLUMN":"Person-Fictional"}`}, nil)
	n, _, e := c.Consume(context.Background(), f)
	if e != nil {
		t.Fatal(e)
	}
	return n
}

func TestT05_15MailRetryRebuildRetainsEveryAttemptAndAcceptanceIsNotDelivery(t *testing.T) {
	r, db, s, consumer := notificationFixture(t)
	n := enqueueMail(t, consumer)
	clock := &mailClock{n.CreatedAt.Add(time.Second)}
	m := &mockMail{send: func(notification.Mail) (notification.Receipt, error) {
		return notification.Receipt{Rejected: true}, nil
	}}
	config := deliveryConfig()
	d := sender(t, r, s, clock, m, config)
	config.TrustedFields[0] = "Person_Fictional" // The constructed sender owns its allowlist copy.
	if count, e := d.Pump(context.Background()); count != 0 || !errors.Is(e, notification.ErrDelivery) {
		t.Fatal(count, e)
	}
	row, p := loadNotice(t, r, n.ID)
	if row.Status != "pending" || row.Attempts != 1 || p.Delivery.Attempts[0].Result != "rejected" || row.SentAt != nil {
		t.Fatal("failed attempt lost")
	}
	if _, e := d.Pump(context.Background()); e != nil || len(m.calls) != 1 {
		t.Fatal("retry delay bypassed")
	}
	clock.now = clock.now.Add(time.Second)
	m.send = func(notification.Mail) (notification.Receipt, error) {
		return notification.Receipt{Accepted: true}, nil
	}
	rebuilt := r
	rebuilt.Database = db
	if count, e := sender(t, rebuilt, s, clock, m, deliveryConfig()).Pump(context.Background()); count != 1 || e != nil {
		t.Fatal(count, e)
	}
	row, p = loadNotice(t, r, n.ID)
	if row.Status != "accepted" || row.SentAt == nil || p.Delivery.DeliveredAt != nil || len(p.Delivery.Attempts) != 2 || p.Delivery.Attempts[0].Result != "rejected" || p.Delivery.Attempts[1].Result != "accepted" {
		t.Fatal("acceptance/delivery/history conflated")
	}
	if m.calls[0].ID != m.calls[1].ID || m.calls[0].Body != m.calls[1].Body || strings.Contains(m.calls[0].Body, "Person-Fictional") || !strings.Contains(m.calls[0].Body, "NEW_COLUMN") {
		t.Fatal("mail identity/privacy changed")
	}
	f, _ := sourceFinal(t, ingest.Full, []string{`{"XQH":"01","NEW_COLUMN":"Person-Fictional"}`}, nil)
	duplicate, created, e := consumer.Consume(context.Background(), f)
	if e != nil || created || duplicate.Status != "accepted" {
		t.Fatal("replay reopened delivered outbox", e)
	}
	if _, e := d.Pump(context.Background()); e != nil || len(m.calls) != 2 {
		t.Fatal("accepted notice resent")
	}
}
func TestT05_15LostChannelReplyReusesStableKeyAndDoesNotInventDelivery(t *testing.T) {
	r, _, s, c := notificationFixture(t)
	n := enqueueMail(t, c)
	clock := &mailClock{n.CreatedAt.Add(time.Second)}
	accepted := map[string]bool{}
	attempt := 0
	m := &mockMail{send: func(v notification.Mail) (notification.Receipt, error) {
		accepted[v.ID] = true
		attempt++
		if attempt == 1 {
			return notification.Receipt{}, errors.New("mail password secret")
		}
		return notification.Receipt{Accepted: true}, nil
	}}
	d := sender(t, r, s, clock, m, deliveryConfig())
	_, e := d.Pump(context.Background())
	if !errors.Is(e, notification.ErrDelivery) || strings.Contains(e.Error(), "password") {
		t.Fatal(e)
	}
	_, p := loadNotice(t, r, n.ID)
	if p.Delivery.Attempts[0].Result != "unknown" {
		t.Fatal("lost response invented rejection")
	}
	clock.now = clock.now.Add(time.Second)
	if _, e = d.Pump(context.Background()); e != nil {
		t.Fatal(e)
	}
	_, p = loadNotice(t, r, n.ID)
	if len(accepted) != 1 || len(m.calls) != 2 || p.Delivery.DeliveredAt != nil {
		t.Fatal("unstable idempotency key or invented delivery")
	}
}
func TestT05_15CompletionCommitReplyLostDoesNotResendAcceptedMail(t *testing.T) {
	r, db, s, c := notificationFixture(t)
	n := enqueueMail(t, c)
	clock := &mailClock{n.CreatedAt.Add(time.Second)}
	m := &mockMail{send: func(notification.Mail) (notification.Receipt, error) {
		db.lostReply = true
		return notification.Receipt{Accepted: true}, nil
	}}
	d := sender(t, r, s, clock, m, deliveryConfig())
	if _, e := d.Pump(context.Background()); !errors.Is(e, notification.ErrOutbox) {
		t.Fatal(e)
	}
	row, p := loadNotice(t, r, n.ID)
	if row.Status != "accepted" || p.Delivery.Attempts[0].Result != "accepted" {
		t.Fatal("committed acceptance missing")
	}
	if _, e := d.Pump(context.Background()); e != nil || len(m.calls) != 1 {
		t.Fatal("lost commit ack resent mail")
	}
}
func TestT05_15ClaimCommitReplyLostExpiresAndPreservesUnknownHistory(t *testing.T) {
	r, db, s, c := notificationFixture(t)
	n := enqueueMail(t, c)
	clock := &mailClock{n.CreatedAt.Add(time.Second)}
	m := &mockMail{send: func(notification.Mail) (notification.Receipt, error) {
		return notification.Receipt{Accepted: true}, nil
	}}
	// The candidate read also commits; inject lost acknowledgement specifically on the claim.
	store := &loseClaimStore{Repository: r, db: db}
	d, e := notification.NewSender(store, m, clock, s, deliveryConfig())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = d.Pump(context.Background()); !errors.Is(e, notification.ErrOutbox) || len(m.calls) != 0 {
		t.Fatal("unacknowledged claim sent", e)
	}
	row, p := loadNotice(t, r, n.ID)
	if row.Status != "sending" || p.Delivery.Attempts[0].Result != "unknown" {
		t.Fatal("claim not durable")
	}
	clock.now = clock.now.Add(5 * time.Second)
	if _, e = sender(t, r, s, clock, m, deliveryConfig()).Pump(context.Background()); e != nil {
		t.Fatal(e)
	}
	_, p = loadNotice(t, r, n.ID)
	if len(p.Delivery.Attempts) != 2 || p.Delivery.Attempts[0].Result != "unknown" {
		t.Fatal("expired unknown claim not recovered")
	}
}

type loseClaimStore struct {
	control.Repository
	db *outboxDB
}

func (s *loseClaimStore) AdvanceNotification(ctx context.Context, a, b control.Notification) (bool, error) {
	s.db.lostReply = true
	return s.Repository.AdvanceNotification(ctx, a, b)
}
func TestT05_15ExhaustedRetainsHistoryAndExplicitBudgetIncreaseResumes(t *testing.T) {
	r, _, s, c := notificationFixture(t)
	n := enqueueMail(t, c)
	clock := &mailClock{n.CreatedAt.Add(time.Second)}
	m := &mockMail{send: func(notification.Mail) (notification.Receipt, error) {
		return notification.Receipt{Rejected: true}, nil
	}}
	config := deliveryConfig()
	config.MaxAttempts = 1
	d := sender(t, r, s, clock, m, config)
	_, _ = d.Pump(context.Background())
	clock.now = clock.now.Add(time.Minute)
	_, _ = d.Pump(context.Background())
	row, p := loadNotice(t, r, n.ID)
	if row.Status != "pending" || !p.Delivery.Exhausted || len(m.calls) != 1 || len(p.Delivery.Attempts) != 1 {
		t.Fatal("exhaustion lost")
	}
	config.MaxAttempts = 2
	m.send = func(notification.Mail) (notification.Receipt, error) {
		delivered := clock.now
		return notification.Receipt{Accepted: true, DeliveredAt: &delivered}, nil
	}
	if _, e := sender(t, r, s, clock, m, config).Pump(context.Background()); e != nil {
		t.Fatal(e)
	}
	row, p = loadNotice(t, r, n.ID)
	if row.Status != "delivered" || p.Delivery.DeliveredAt == nil || p.Delivery.Exhausted || len(p.Delivery.Attempts) != 2 {
		t.Fatal("explicit recovery/history/delivery failed")
	}
	config.MaxAttempts = 17
	if _, e := notification.NewSender(r, m, clock, s, config); !errors.Is(e, notification.ErrConfiguration) {
		t.Fatal("hard bound bypassed")
	}
}
func TestT05_15CompletionFailureRecoversWithSameIDAndStaleCompletionIsFenced(t *testing.T) {
	r, db, s, c := notificationFixture(t)
	n := enqueueMail(t, c)
	clock := &mailClock{n.CreatedAt.Add(time.Second)}
	m := &mockMail{send: func(notification.Mail) (notification.Receipt, error) {
		db.failCommit = true
		return notification.Receipt{Accepted: true}, nil
	}}
	if _, e := sender(t, r, s, clock, m, deliveryConfig()).Pump(context.Background()); !errors.Is(e, notification.ErrOutbox) {
		t.Fatal(e)
	}
	db.failCommit = false
	old, p := loadNotice(t, r, n.ID)
	if old.Status != "sending" || p.Delivery.Attempts[0].Result != "unknown" {
		t.Fatal("uncommitted result presented as fact")
	}
	clock.now = clock.now.Add(5 * time.Second)
	m.send = func(notification.Mail) (notification.Receipt, error) {
		return notification.Receipt{Accepted: true}, nil
	}
	if _, e := sender(t, r, s, clock, m, deliveryConfig()).Pump(context.Background()); e != nil {
		t.Fatal(e)
	}
	stale := old
	stale.Status = "accepted"
	stale.NextAttemptAt = nil
	stale.SentAt = &clock.now
	if changed, e := r.AdvanceNotification(context.Background(), old, stale); e != nil || changed {
		t.Fatal("stale completion overwrote new claim", e)
	}
	if len(m.calls) != 2 || m.calls[0].ID != m.calls[1].ID {
		t.Fatal("retry key changed")
	}
}
func TestT05_15MissingTargetsAndConfigurationDiagnosticsAreSafe(t *testing.T) {
	r, _, s, _ := notificationFixture(t)
	clock := &mailClock{time.Now()}
	m := &mockMail{}
	config := deliveryConfig()
	config.To = ""
	if _, e := notification.NewSender(r, m, clock, s, config); !errors.Is(e, notification.ErrConfiguration) {
		t.Fatal("missing mail target silently disabled")
	}
	config.To = "secret@example.invalid\r\nBcc:bad@example.invalid"
	if _, e := notification.NewSender(r, m, clock, s, config); !errors.Is(e, notification.ErrConfiguration) {
		t.Fatal("header injection")
	}
	if strings.Contains(fmt.Sprintf("%#v %#v", config, &config), "secret") {
		t.Fatal("configuration leaked")
	}
}

func TestT05_15HardBoundRetainsAllSixteenResults(t *testing.T) {
	r, _, s, c := notificationFixture(t)
	n := enqueueMail(t, c)
	clock := &mailClock{n.CreatedAt.Add(time.Second)}
	m := &mockMail{send: func(notification.Mail) (notification.Receipt, error) {
		return notification.Receipt{Rejected: true}, nil
	}}
	config := deliveryConfig()
	config.MaxAttempts = 16
	d := sender(t, r, s, clock, m, config)
	for i := 0; i < 20; i++ {
		_, _ = d.Pump(context.Background())
		clock.now = clock.now.Add(time.Second)
	}
	row, p := loadNotice(t, r, n.ID)
	if row.Status != "pending" || row.Attempts != 16 || len(p.Delivery.Attempts) != 16 || len(m.calls) != 16 || !p.Delivery.Exhausted {
		t.Fatal("hard bound lost history or sent again")
	}
	for i, a := range p.Delivery.Attempts {
		if a.Number != i+1 || a.Result != "rejected" || a.FinishedAt == nil {
			t.Fatal("attempt result missing")
		}
	}
}

type atomicMailClock struct{ n atomic.Int64 }

func (c *atomicMailClock) Now() time.Time { return time.Unix(0, c.n.Load()).UTC() }

type mailFunc func(context.Context, notification.Mail) (notification.Receipt, error)

func (f mailFunc) Send(ctx context.Context, m notification.Mail) (notification.Receipt, error) {
	return f(ctx, m)
}
func TestT05_15LateSenderCannotOverwriteNewClaim(t *testing.T) {
	r, _, s, c := notificationFixture(t)
	n := enqueueMail(t, c)
	clock := &atomicMailClock{}
	clock.n.Store(n.CreatedAt.Add(time.Second).UnixNano())
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	channel := mailFunc(func(ctx context.Context, _ notification.Mail) (notification.Receipt, error) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return notification.Receipt{}, ctx.Err()
			}
			return notification.Receipt{Rejected: true}, nil
		}
		return notification.Receipt{Accepted: true}, nil
	})
	d, e := notification.NewSender(r, channel, clock, s, deliveryConfig())
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := d.Pump(context.Background()); done <- e }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first send not entered")
	}
	clock.n.Add(int64(5 * time.Second))
	if _, e = d.Pump(context.Background()); e != nil {
		close(release)
		t.Fatal(e)
	}
	close(release)
	if e = <-done; !errors.Is(e, notification.ErrOutbox) {
		t.Fatal("late sender not fenced", e)
	}
	row, p := loadNotice(t, r, n.ID)
	if row.Status != "accepted" || row.Attempts != 2 || p.Delivery.Attempts[0].Result != "unknown" || p.Delivery.Attempts[1].Result != "accepted" {
		t.Fatal("late rejection corrupted accepted state")
	}
}
func TestT05_15MailRestorationDoesNotTrustStoredFieldNames(t *testing.T) {
	r, db, s, c := notificationFixture(t)
	n := enqueueMail(t, c)
	clock := &mailClock{n.CreatedAt.Add(time.Second)}
	var p notification.Payload
	_ = json.Unmarshal([]byte(n.Payload), &p)
	p.Notice.Fields[0].Name = "Person_Fictional" // Corrupt stored text, leave the trusted reference unchanged.
	b, _ := json.Marshal(p)
	db.rows[n.FailureKey][3] = string(b)
	m := &mockMail{send: func(notification.Mail) (notification.Receipt, error) {
		return notification.Receipt{Accepted: true}, nil
	}}
	config := deliveryConfig()
	d := sender(t, r, s, clock, m, config)
	if _, e := d.Pump(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(m.calls) != 1 || strings.Contains(m.calls[0].Body, "Person_Fictional") {
		t.Fatal("untrusted stored name leaked")
	}
	config.TrustedFields = []string{"TOPSECRET_TOKEN"}
	if _, e := notification.NewSender(r, m, clock, s, config); !errors.Is(e, notification.ErrConfiguration) {
		t.Fatal("secret trusted name permitted")
	}
}
