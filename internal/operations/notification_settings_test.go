package operations

import (
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"strings"
	"testing"
	"time"
)

func TestT33_02SettingsLimitsAndLocalPartCase(t *testing.T) {
	for _, tc := range []struct {
		address string
		valid   bool
	}{
		{strings.Repeat("a", 242) + "@example.com", true}, {strings.Repeat("a", 243) + "@example.com", false},
		{"Display <a@example.invalid>", false}, {"a@example.invalid\r\n", false}, {"a@EXAMPLE.INVALID", true},
	} {
		_, err := NormalizeAddress(tc.address)
		if (err == nil) != tc.valid {
			t.Fatal("address limit", len(tc.address), err)
		}
	}
	s := NotificationSettings{Revision: 1, Enabled: true, Recipients: []string{"A@EXAMPLE.INVALID", "A@example.invalid", "a@example.invalid"}}
	if s.Normalize(true) != nil || len(s.Recipients) != 2 {
		t.Fatal("local case lost", s)
	}
	s.Recipients = make([]string, 21)
	if s.Normalize(true) != ErrArgument {
		t.Fatal("21 recipients accepted")
	}
	s.Recipients = []string{}
	s.Rules = map[string]bool{"arbitrary": true}
	if s.Normalize(true) != ErrArgument {
		t.Fatal("arbitrary rule accepted")
	}
}
func TestT33_04ChannelObservationNeverClaimsDeliveryOrLeaksReceipt(t *testing.T) {
	n := &Notifier{}
	if d := n.ChannelDiagnostics(); d.Configured || d.ObservedAt != nil || d.LastResult != "not_observed" {
		t.Fatal(d)
	}
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		receipt notification.Receipt
		want    string
	}{{notification.Receipt{Accepted: true}, "accepted"}, {notification.Receipt{Rejected: true}, "rejected"}, {notification.Receipt{Temporary: true}, "temporary_failure"}, {notification.Receipt{}, "unknown"}} {
		n.observeChannel(tc.receipt, at)
		d := n.ChannelDiagnostics()
		raw, _ := json.Marshal(d)
		if d.LastResult != tc.want || d.ObservedAt == nil || !d.ObservedAt.Equal(at) || strings.Contains(string(raw), "delivered") {
			t.Fatal(d)
		}
		*d.ObservedAt = at.Add(time.Hour)
		if !n.ChannelDiagnostics().ObservedAt.Equal(at) {
			t.Fatal("caller mutated observation")
		}
	}
}
