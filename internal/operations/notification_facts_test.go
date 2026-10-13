package operations

import (
	"strings"
	"testing"
	"time"
)

func TestT33_01And06DisabledFactsPersistAndReplayNeverExpandsRecipients(t *testing.T) {
	for _, replayed := range []bool{false, true} {
		facts, deliveries := 0, 0
		tx := &fakeTx{exec: func(q string, a []any) (int64, error) {
			if strings.Contains(q, "notification_facts") {
				facts++
				if replayed {
					return 0, nil
				}
				return 1, nil
			}
			deliveries++
			return 1, nil
		}}
		s := NotificationSettings{Revision: 3, Enabled: replayed, Recipients: []string{"new@example.invalid"}, Rules: map[string]bool{"task_failed": true}}
		if e := fanout(t.Context(), tx, s, "task:synthetic", "task_failed", time.Now(), time.Now()); e != nil || facts != 1 || deliveries != 0 {
			t.Fatal("old/disabled fact expanded", e)
		}
	}
}

func TestT33_10NotificationDeadlineStartsAtQueueTimeNotOldFactTime(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	queued := at.Add(7 * 24 * time.Hour)
	writes := 0
	tx := &fakeTx{exec: func(q string, a []any) (int64, error) {
		if strings.Contains(q, "notification_facts") {
			if !a[2].(time.Time).Equal(at) {
				t.Fatal("changed fact time")
			}
		} else {
			writes++
			if !a[5].(time.Time).Equal(queued) || !a[6].(time.Time).Equal(queued.Add(24*time.Hour)) {
				t.Fatal("budget did not start at enqueue")
			}
		}
		return 1, nil
	}}
	s := NotificationSettings{Revision: 1, Enabled: true, Recipients: []string{"a@example.invalid", "b@example.invalid"}, Rules: map[string]bool{"task_failed": true}}
	if e := fanout(t.Context(), tx, s, "task:synthetic", "task_failed", at, queued); e != nil || writes != 2 {
		t.Fatal(e)
	}
}
