//go:build integration

package integration

import (
	"context"
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"sync"
	"testing"
	"time"
)

type recordingMail struct {
	mu    sync.Mutex
	mails []notification.Mail
}

func (m *recordingMail) Send(_ context.Context, v notification.Mail) (notification.Receipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mails = append(m.mails, v)
	return notification.Receipt{Accepted: true}, nil
}

func TestT33_18And19NotifierRebuildDynamicRecipientsAndUnavailableLogs(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	c := e.connection(t, 4)
	repo := operations.Repository{Registry: e.registry, Database: e.db}
	now := time.Now().UTC()
	channel := &recordingMail{}
	failLogs := events.SinkFunc(func(context.Context, events.Event) error { return errors.New("both log destinations unavailable") })
	makeNotifier := func(owner string) *operations.Notifier {
		return &operations.Notifier{Repository: repo, Channel: channel, Audit: failLogs, Owner: owner, From: "sender@example.invalid", LegacyTo: "old@example.invalid"}
	}
	first := makeNotifier("first")
	if err := first.Ensure(e.ctx); err != nil {
		t.Fatal(err)
	}
	settings, err := repo.NotificationSettings(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.Rules["task_failed"] = true
	settings.Recipients = []string{"a@example.invalid"}
	if settings, err = repo.SaveNotifications(e.ctx, settings, true, "admin", now); err != nil {
		t.Fatal(err)
	}
	seed := func(id string) {
		t.Helper()
		if _, err := c.Exec(e.ctx, `INSERT INTO control.tasks(id,idempotency_key,mode,status,requested_by,accepted_at,finished_at,summary) VALUES($1,$1,'initialize','failed','synthetic',$2,$2,'{}')`, id, now); err != nil {
			t.Fatal(err)
		}
	}
	pump := func(n *operations.Notifier) {
		t.Helper()
		if err := n.Pump(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
	seed("before-rebuild")
	pump(first)
	// A reconstructed instance must read current saved settings, not legacy env.
	settings.Recipients = append(settings.Recipients, "b@example.invalid")
	if settings, err = repo.SaveNotifications(e.ctx, settings, true, "admin", now); err != nil {
		t.Fatal(err)
	}
	rebuilt := makeNotifier("rebuilt")
	pump(rebuilt)
	if len(channel.mails) != 1 || channel.mails[0].To != "a@example.invalid" {
		t.Fatal("old facts expanded after address addition", channel.mails)
	}
	seed("after-rebuild")
	results := make(chan error, 2)
	for _, n := range []*operations.Notifier{first, rebuilt} {
		go func() { results <- n.Pump(e.ctx) }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if len(channel.mails) != 3 {
		t.Fatal("concurrent fanout/claim", channel.mails)
	}
	ids := map[string]bool{}
	for _, v := range channel.mails {
		if ids[v.ID] {
			t.Fatal("duplicate send", v.ID)
		}
		ids[v.ID] = true
	}
	settings.Enabled = false
	if settings, err = repo.SaveNotifications(e.ctx, settings, true, "admin", now); err != nil {
		t.Fatal(err)
	}
	seed("while-disabled")
	pump(rebuilt)
	settings.Enabled = true
	settings.Recipients = []string{"b@example.invalid"}
	if settings, err = repo.SaveNotifications(e.ctx, settings, true, "admin", now); err != nil {
		t.Fatal(err)
	}
	pump(makeNotifier("third"))
	if len(channel.mails) != 3 {
		t.Fatal("reopened old disabled fact")
	}
	seed("after-enable")
	pump(rebuilt)
	if len(channel.mails) != 4 || channel.mails[3].To != "b@example.invalid" {
		t.Fatal("removed address retained", channel.mails)
	}
	var pending int
	if err = c.QueryRow(e.ctx, "SELECT count(*) FROM control.management_audit WHERE NOT confirmed").Scan(&pending); err != nil || pending < 4 {
		t.Fatal("lost audit during log outage", pending, err)
	}
	// Restore the destination; deterministic IDs permit idempotent external writes.
	observed := map[string]bool{}
	rebuilt.Audit = events.SinkFunc(func(_ context.Context, v events.Event) error {
		if v.Audit == nil || v.Audit.ActorID == "admin" {
			t.Fatal("unredacted audit")
		}
		observed[v.ID] = true
		return nil
	})
	pump(rebuilt)
	if err = c.QueryRow(e.ctx, "SELECT count(*) FROM control.management_audit WHERE NOT confirmed").Scan(&pending); err != nil || pending != 0 || len(observed) < 4 {
		t.Fatal("audit recovery", pending, observed, err)
	}
	if len(channel.mails) != 4 {
		t.Fatal("audit recovery sent duplicate mail")
	}
	job, err := repo.AcceptNotificationTest(e.ctx, "admin", "explicit-runtime", settings.Revision, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	pump(rebuilt)
	done, err := repo.Job(e.ctx, job.ID, "admin", "notification_test")
	if err != nil || done.Status != "succeeded" || len(channel.mails) != 5 || channel.mails[4].To != "b@example.invalid" {
		t.Fatal("saved test address", done, channel.mails, err)
	}
	pump(rebuilt)
	if len(channel.mails) != 5 {
		t.Fatal("test replay sent twice")
	}

}
