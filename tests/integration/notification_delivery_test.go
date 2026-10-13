//go:build integration

package integration

import (
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"testing"
	"time"
)

func TestT33_06To11PostgresClaimCloseRecoveryAndBudgets(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	c := e.connection(t, 4)
	repo := operations.Repository{Registry: e.registry, Database: e.db}
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	if err := repo.EnsureNotifications(e.ctx, "", true, at); err != nil {
		t.Fatal(err)
	}
	settings, err := repo.SaveNotifications(e.ctx, operations.NotificationSettings{Revision: 1, Enabled: true, Recipients: []string{"a@example.invalid", "b@example.invalid"}, Rules: map[string]bool{"task_failed": true}}, true, "admin", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(e.ctx, `INSERT INTO control.tasks(id,idempotency_key,mode,status,requested_by,accepted_at,finished_at,summary) VALUES('failed','failed','initialize','failed','synthetic',$1,$1,'{}')`, at); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = repo.NotificationFacts(e.ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = c.QueryRow(e.ctx, "SELECT count(*) FROM control.notification_deliveries").Scan(&count); err != nil || count != 2 {
		t.Fatal("fanout replay", count, err)
	}
	type result struct {
		d   *operations.Delivery
		err error
	}
	results := make(chan result, 2)
	for _, owner := range []string{"worker-a", "worker-b"} {
		go func() { d, err := repo.ClaimDelivery(e.ctx, owner, at); results <- result{d, err} }()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.d == nil || second.d == nil || first.d.ID == second.d.ID {
		t.Fatal("duplicate claim", first, second)
	}
	// Closing after claims cannot retract observed acceptance; retry is cancelled.
	settings.Enabled = false
	settings, err = repo.SaveNotifications(e.ctx, settings, true, "admin", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishDelivery(e.ctx, *first.d, notification.Receipt{Accepted: true}, nil, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishDelivery(e.ctx, *second.d, notification.Receipt{Temporary: true}, errors.New("synthetic temporary"), at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{first.d.ID: "accepted", second.d.ID: "cancelled"} {
		var got string
		if err = c.QueryRow(e.ctx, "SELECT status FROM control.notification_deliveries WHERE id=$1", id).Scan(&got); err != nil || got != want {
			t.Fatal(got, want, err)
		}
	}
	if d, err := repo.ClaimDelivery(e.ctx, "closed-worker", at.Add(time.Hour)); err != nil || d != nil {
		t.Fatal("closed channel claimed", d, err)
	}
	// Close before claim wins under the same settings row lock, including retry.
	settings.Enabled = true
	settings, err = repo.SaveNotifications(e.ctx, settings, true, "admin", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(e.ctx, `INSERT INTO control.notification_facts(id,code,occurred_at,decision,revision) VALUES('budget','test',$1,'queued',1)`, at); err != nil {
		t.Fatal(err)
	}
	seed := func(id string, deadline time.Time) {
		t.Helper()
		if _, err := c.Exec(e.ctx, `INSERT INTO control.notification_facts(id,code,occurred_at,decision,revision) VALUES($1,'test',$2,'queued',1)`, id, at); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Exec(e.ctx, `INSERT INTO control.notification_deliveries(id,fact_id,rule_id,revision,recipient,status,created_at,deadline) VALUES($1,$1,'test',1,'a@example.invalid','queued',$2,$3)`, id, at, deadline); err != nil {
			t.Fatal(err)
		}
	}
	seed("close-first", at.Add(24*time.Hour))
	settings.Enabled = false
	settings, err = repo.SaveNotifications(e.ctx, settings, true, "admin", at)
	if err != nil {
		t.Fatal(err)
	}
	if d, err := repo.ClaimDelivery(e.ctx, "worker", at); err != nil || d != nil {
		t.Fatal("claim after close", d, err)
	}
	settings.Enabled = true
	settings, err = repo.SaveNotifications(e.ctx, settings, true, "admin", at)
	if err != nil {
		t.Fatal(err)
	}
	seed("retry-budget", at.Add(24*time.Hour))
	now := at
	for attempt := 1; attempt <= 3; attempt++ {
		d, err := repo.ClaimDelivery(e.ctx, "worker", now)
		if err != nil || d == nil || d.ID != "retry-budget" || d.Attempts != attempt {
			t.Fatal("attempt budget", d, err)
		}
		if err = repo.FinishDelivery(e.ctx, *d, notification.Receipt{Temporary: true}, errors.New("synthetic"), now); err != nil {
			t.Fatal(err)
		}
		delay := time.Minute
		if attempt == 2 {
			delay = 5 * time.Minute
		}
		if next, err := repo.ClaimDelivery(e.ctx, "early", now.Add(delay-time.Nanosecond)); err != nil || next != nil {
			t.Fatal("early retry", next, err)
		}
		now = now.Add(delay)
	}
	if d, err := repo.ClaimDelivery(e.ctx, "fourth", now); err != nil || d != nil {
		t.Fatal("fourth attempt", d, err)
	}
	seed("expired", at.Add(24*time.Hour))
	if d, err := repo.ClaimDelivery(e.ctx, "deadline", at.Add(24*time.Hour)); err != nil || d != nil {
		t.Fatal("24 hour boundary", d, err)
	}
	seed("lost-worker", at.Add(24*time.Hour))
	d, err := repo.ClaimDelivery(e.ctx, "old-worker", at)
	if err != nil || d == nil {
		t.Fatal(err)
	}
	if next, err := repo.ClaimDelivery(e.ctx, "replacement", at.Add(time.Minute)); err != nil || next != nil {
		t.Fatal("sending was resent", next, err)
	}
	if err = repo.FinishDelivery(e.ctx, *d, notification.Receipt{Accepted: true}, nil, at.Add(time.Minute)); err != operations.ErrConflict {
		t.Fatal("stale sender overwrote unknown", err)
	}
	var status string
	if err = c.QueryRow(e.ctx, "SELECT status FROM control.notification_deliveries WHERE id='lost-worker'").Scan(&status); err != nil || status != "unknown" {
		t.Fatal(status, err)
	}
}
