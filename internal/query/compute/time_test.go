package compute_test

import (
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
)

func str(v string) *string { return &v }

func TestT06_13ExplicitSourceZoneIgnoresMachineDefault(t *testing.T) {
	previous := time.Local
	t.Cleanup(func() { time.Local = previous })
	for _, offset := range []int{0, -7 * 3600, 9 * 3600} {
		time.Local = time.FixedZone("machine", offset)
		formatter, err := compute.NewTimes("Asia/Shanghai")
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct{ raw, want string }{
			{"2026-09-18 08:30:00", "2026-09-18T08:30:00+08:00"},
			{"2026-09-18T08:30:00.123456", "2026-09-18T08:30:00.123456+08:00"},
			{"2026-09-18T00:30:00Z", "2026-09-18T08:30:00+08:00"},
			{"2026-09-18T09:30:00+09:00", "2026-09-18T08:30:00+08:00"},
		} {
			got, err := formatter.Source(&tc.raw)
			if err != nil || got == nil || *got != tc.want {
				t.Fatal(got, err)
			}
		}
	}
}

func TestT06_13MissingSourceTimeAndConfirmedSyncRemainDistinct(t *testing.T) {
	f, err := compute.NewTimes("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.Source(nil); err != nil || got != nil {
		t.Fatal(got, err)
	}
	if got, err := f.Latest(nil, nil); err != nil || got != nil {
		t.Fatal(got, err)
	}
	got, err := f.Latest(nil, str("2026-09-18 07:30:00"), str("2026-09-18T00:30:00Z"))
	if err != nil || got == nil || *got != "2026-09-18T08:30:00+08:00" {
		t.Fatal(got, err)
	}
	synced, err := f.Instant(time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC))
	if err != nil || synced == nil || *synced != "2026-09-18T09:00:00+08:00" || *synced == *got {
		t.Fatal(synced, err)
	}
	if got, err := f.Instant(time.Time{}); err == nil || got != nil {
		t.Fatal("missing confirmation invented time")
	}
}

func TestT06_13InvalidPresentTimesFail(t *testing.T) {
	for _, zone := range []string{"", "Local", "unknown/timezone"} {
		if _, err := compute.NewTimes(zone); err == nil {
			t.Fatal(zone)
		}
	}
	f, _ := compute.NewTimes("Asia/Shanghai")
	for _, raw := range []string{"", " ", "2026-02-30 01:00:00", "2026-09-18", "0000-01-01T00:00:00", "2026-01-01T00:00:00.1234567"} {
		if got, err := f.Source(&raw); err == nil || got != nil {
			t.Fatal(raw, got, err)
		}
	}
	dst, _ := compute.NewTimes("America/New_York")
	if got, err := dst.Source(str("2026-03-08 02:30:00")); err == nil || got != nil {
		t.Fatal("nonexistent local time accepted")
	}
	if got, err := f.Latest(str("2026-09-18 08:30:00"), str("bad")); err == nil || got != nil {
		t.Fatal("partial latest time")
	}
}
