package operations

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func TestT33_05And19LegacyHistorySurvivesDisabledChannel(t *testing.T) {
	settingsInsert, historyInsert, attemptInsert := false, false, false
	tx := &fakeTx{exec: func(q string, a []any) (int64, error) {
		switch {
		case strings.Contains(q, "INSERT INTO control.notification_settings"):
			settingsInsert = true
			if a[0] != false || a[1] != "[]" {
				t.Fatal("history enabled transport")
			}
		case strings.Contains(q, "INSERT INTO control.notification_deliveries"):
			historyInsert = true
			if a[0] != "User@example.invalid" || !strings.Contains(q, "THEN 'rejected'") || strings.Contains(q, "THEN 'queued'") {
				t.Fatal("legacy outcome/address lost or requeued")
			}
		case strings.Contains(q, "INSERT INTO control.notification_attempts"):
			attemptInsert = true
		}
		return 1, nil
	}}
	if err := fixtureRepo(tx).EnsureNotifications(t.Context(), "User@EXAMPLE.INVALID", false, time.Now()); err != nil || !settingsInsert || !historyInsert || !attemptInsert {
		t.Fatal(err)
	}
	// Subsequent boots cannot overwrite the administrator's settings or budget.
	calls := 0
	tx.exec = func(q string, a []any) (int64, error) {
		calls++
		if !strings.Contains(q, "notification_settings") {
			t.Fatal("history reimported")
		}
		return 0, nil
	}
	if err := fixtureRepo(tx).EnsureNotifications(t.Context(), "new@example.invalid", true, time.Now()); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestT33_08And11RemovedRecipientCancelsRetryButNotObservedAcceptance(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		now := time.Now()
		owner := "worker"
		status := ""
		tx := &fakeTx{row: func(q string, a []any) storage.Row {
			return fakeRow(func(d ...any) error {
				if !strings.HasSuffix(q, "FOR UPDATE") {
					t.Fatal("completion not ordered with settings changes")
				}
				*d[0].(*int64), *d[1].(*bool), *d[2].(*string), *d[3].(*string), *d[4].(*time.Time) = 2, false, `[]`, `{}`, now
				return nil
			})
		}, exec: func(q string, a []any) (int64, error) {
			if strings.Contains(q, "UPDATE control.notification_deliveries") {
				status = a[3].(string)
				if a[4].(*time.Time) != nil {
					t.Fatal("removed recipient retried")
				}
			}
			return 1, nil
		}}
		d := Delivery{ID: "delivery", Rule: "task_failed", Recipient: "old@example.invalid", Attempts: 1, Owner: &owner, Deadline: now.Add(time.Hour)}
		r := notification.Receipt{Temporary: !accepted, Accepted: accepted}
		if err := fixtureRepo(tx).FinishDelivery(t.Context(), d, r, nil, now); err != nil {
			t.Fatal(err)
		}
		want := "cancelled"
		if accepted {
			want = "accepted"
		}
		if status != want {
			t.Fatal("settings change rewrote external outcome", status)
		}
	}
}

func TestT33_09And10LateSenderCannotOverwriteRecoveredUnknown(t *testing.T) {
	now := time.Now()
	owner := "old-worker"
	attemptUpdated := false
	tx := &fakeTx{row: func(string, []any) storage.Row { return storedSettings() }, exec: func(q string, a []any) (int64, error) {
		if strings.Contains(q, "UPDATE control.notification_deliveries") {
			if !strings.Contains(q, "status='sending'") || !strings.Contains(q, "lease_until>$7") || !strings.Contains(q, "attempts=$3") || !strings.Contains(q, "owner=$2") {
				t.Fatal("delivery completion unfenced")
			}
			return 0, nil
		}
		attemptUpdated = true
		return 1, nil
	}}
	d := Delivery{ID: "delivery", Rule: "task_failed", Recipient: "a@example.invalid", Attempts: 1, Owner: &owner, Deadline: now.Add(time.Hour)}
	if err := fixtureRepo(tx).FinishDelivery(t.Context(), d, notification.Receipt{Accepted: true}, nil, now); err != ErrConflict || attemptUpdated || tx.committed {
		t.Fatal("late acceptance overwrote unknown", err)
	}
}

func TestT33_16DecisionsPreserveDisabledAndPartialOutcomes(t *testing.T) {
	at := time.Now()
	rows := &viewRows{items: [][]any{{"task:a", "task_failed", "disabled", int64(1), at, int64(0), int64(0), int64(0), int64(0)}, {"task:b", "task_failed", "queued", int64(2), at, int64(3), int64(1), int64(1), int64(1)}}}
	tx := &viewTx{query: func(q string, a []any) (storage.Rows, error) {
		if a[0] != "task:0" || a[1] != 2 || !strings.Contains(q, "LEFT JOIN") {
			t.Fatal("decision paging dropped disabled facts")
		}
		return rows, nil
	}}
	got, err := (Repository{Database: viewDB{tx}}).Decisions(t.Context(), "task:0", 2)
	if err != nil || len(got) != 2 || got[0].Deliveries != 0 || got[0].Decision != "disabled" || got[1].Accepted != 1 || got[1].Unknown != 1 || !rows.closed {
		t.Fatal("decision misreported", got, err)
	}
}

func TestT33_15ExpiredTestExecutorCannotFanout(t *testing.T) {
	now := time.Now()
	owner := "worker"
	j := Job{ID: "explicit-test", Subject: "actor", Action: "notification_test", Request: json.RawMessage(`{"revision":"1"}`), Summary: json.RawMessage(`{}`), Deadline: now.Add(time.Second), Owner: &owner}
	lock := false
	fenced := false
	fanouts := 0
	tx := &fakeTx{row: func(q string, a []any) storage.Row {
		if strings.Contains(q, "SELECT (SELECT id") {
			return fakeRow(func(d ...any) error { *d[0].(**string) = &j.ID; return nil })
		}
		if strings.Contains(q, "SELECT EXISTS") {
			return fakeRow(func(d ...any) error {
				if !lock || !strings.Contains(q, "deadline>clock_timestamp()") || !strings.Contains(q, "lease_until>clock_timestamp()") {
					t.Fatal("missing executor fence")
				}
				fenced = true
				*d[0].(*bool) = false
				return nil
			})
		}
		return jobRow(j)
	}, exec: func(q string, a []any) (int64, error) {
		if strings.Contains(q, "pg_advisory_xact_lock") {
			lock = true
		}
		if strings.Contains(q, "INSERT INTO control.notification") {
			fanouts++
		}
		return 1, nil
	}}
	n := &Notifier{Repository: fixtureRepo(tx), Owner: owner}
	if err := n.test(context.Background()); err != nil || !fenced || fanouts != 0 {
		t.Fatal("expired executor produced mail", err, fanouts)
	}
}
