//go:build integration

package integration

import (
	"fmt"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"testing"
	"time"
)

func TestT33_05PostgresLegacyConfigurationAndHistoryMatrix(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	c := e.connection(t, 4)
	repo := operations.Repository{Registry: e.registry, Database: e.db}
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	if _, err := c.Exec(e.ctx, `INSERT INTO control.tasks(id,idempotency_key,mode,status,requested_by,accepted_at,summary) VALUES('legacy-task','legacy-key','initialize','failed','synthetic',$1,'{}')`, now); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []struct {
		name, address string
		channel       bool
		enabled       bool
	}{
		{"valid", "User@EXAMPLE.INVALID", true, true}, {"disabled", "User@EXAMPLE.INVALID", false, false}, {"missing", "", false, false}, {"invalid", "not-an-address", true, false},
	} {
		t.Run(cfg.name, func(t *testing.T) {
			if _, err := c.Exec(e.ctx, "TRUNCATE control.notification_attempts,control.notification_deliveries,control.notification_facts,control.notification_settings,control.notification_outbox"); err != nil {
				t.Fatal(err)
			}
			states := []struct {
				id, status, payload, want string
				attempts                  int
			}{
				{"untouched", "pending", "{}", "", 0},
				{"accepted", "accepted", "{}", "accepted", 1}, {"delivered", "delivered", "{}", "accepted", 2},
				{"rejected", "pending", `{"delivery":{"attempts":[{"number":1,"started_at":"2026-10-12T00:00:00Z","result":"rejected"}]}}`, "rejected", 1},
				{"sending", "sending", `{"delivery":{"attempts":null}}`, "unknown", 1},
				{"retry", "pending", "{}", "unknown", 2}, {"exhausted", "pending", "{}", "exhausted", 3}, {"old-budget", "failed", "{}", "exhausted", 16},
			}
			for _, s := range states {
				if _, err := c.Exec(e.ctx, `INSERT INTO control.notification_outbox(id,failure_key,task_id,payload,status,attempts,created_at) VALUES($1,$1,'legacy-task',$2::jsonb,$3,$4,$5)`, s.id, s.payload, s.status, s.attempts, now.Add(-time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.EnsureNotifications(e.ctx, cfg.address, cfg.channel, now); err != nil {
				t.Fatal(err)
			}
			got, err := repo.NotificationSettings(e.ctx)
			if err != nil || got.Enabled != cfg.enabled || got.Revision != 1 {
				t.Fatal(got, err)
			}
			valid := cfg.address == "User@EXAMPLE.INVALID"
			for _, s := range states {
				var count int
				if err := c.QueryRow(e.ctx, "SELECT count(*) FROM control.notification_deliveries WHERE id=$1", "legacy:"+s.id).Scan(&count); err != nil {
					t.Fatal(err)
				}
				wantCount := 0
				if valid && s.want != "" {
					wantCount = 1
				}
				if count != wantCount {
					t.Fatal(s.id, count, wantCount)
				}
				if count == 1 {
					var status, address string
					var attempts int
					var deadline time.Time
					if err := c.QueryRow(e.ctx, "SELECT status,recipient,attempts,deadline FROM control.notification_deliveries WHERE id=$1", "legacy:"+s.id).Scan(&status, &address, &attempts, &deadline); err != nil || status != s.want || address != "User@example.invalid" || attempts != s.attempts || !deadline.Equal(now.Add(23*time.Hour)) {
						t.Fatal(s.id, status, address, attempts, deadline, err)
					}
				}
			}
			if _, err := c.Exec(e.ctx, "UPDATE control.notification_settings SET revision=7,enabled=false,recipients='[]'"); err != nil {
				t.Fatal(err)
			}
			if err := repo.EnsureNotifications(e.ctx, "new@example.invalid", true, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			got, err = repo.NotificationSettings(e.ctx)
			if err != nil || got.Revision != 7 || got.Enabled || len(got.Recipients) != 0 {
				t.Fatal("reentry overwrote saved configuration", fmt.Sprint(got), err)
			}
			results := make(chan error, 2)
			for _, subject := range []string{"session-a", "session-b"} {
				go func() {
					_, err := repo.SaveNotifications(e.ctx, operations.NotificationSettings{Revision: 7, Recipients: []string{}}, true, subject, now)
					results <- err
				}()
			}
			passed, conflicts := 0, 0
			for range 2 {
				err := <-results
				if err == nil {
					passed++
				} else if err == operations.ErrConflict {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			if passed != 1 || conflicts != 1 {
				t.Fatal("same revision overwrote settings", passed, conflicts)
			}
		})
	}
}
