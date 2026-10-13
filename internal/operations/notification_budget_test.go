package operations

import (
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"strings"
	"testing"
	"time"
)

func TestT33_08And10ExactRetryDeadlineAndAmbiguousReceipt(t *testing.T) {
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	owner := "worker"
	for _, tc := range []struct {
		attempt   int
		remaining time.Duration
		receipt   notification.Receipt
		sendErr   error
		want      string
		delay     time.Duration
	}{
		{1, time.Minute, notification.Receipt{Temporary: true}, nil, "exhausted", 0},
		{1, time.Minute + time.Microsecond, notification.Receipt{Temporary: true}, nil, "retry", time.Minute},
		{2, 5 * time.Minute, notification.Receipt{Temporary: true}, nil, "exhausted", 0},
		{2, 5*time.Minute + time.Microsecond, notification.Receipt{Temporary: true}, nil, "retry", 5 * time.Minute},
		{3, time.Hour, notification.Receipt{Temporary: true}, nil, "exhausted", 0},
		{1, time.Hour, notification.Receipt{Accepted: true, Temporary: true}, nil, "unknown", 0},
		{1, time.Hour, notification.Receipt{Accepted: true}, errors.New("lost response"), "unknown", 0},
	} {
		status := ""
		var next *time.Time
		tx := &fakeTx{row: func(string, []any) storage.Row { return storedSettings() }, exec: func(q string, a []any) (int64, error) {
			if strings.Contains(q, "UPDATE control.notification_deliveries") {
				status = a[3].(string)
				next = a[4].(*time.Time)
			}
			return 1, nil
		}}
		d := Delivery{ID: "delivery", Recipient: "a@example.invalid", Rule: "task_failed", Attempts: tc.attempt, Owner: &owner, Deadline: at.Add(tc.remaining)}
		if err := fixtureRepo(tx).FinishDelivery(t.Context(), d, tc.receipt, tc.sendErr, at); err != nil || status != tc.want {
			t.Fatal(status, err)
		}
		if tc.delay == 0 && next != nil || tc.delay > 0 && (next == nil || !next.Equal(at.Add(tc.delay))) {
			t.Fatal("retry boundary", next)
		}
		n := &Notifier{}
		n.observeChannel(tc.receipt, tc.sendErr, at)
		if tc.want == "unknown" && n.ChannelDiagnostics().LastResult != "unknown" {
			t.Fatal("ambiguous send reported healthy")
		}
	}
}
