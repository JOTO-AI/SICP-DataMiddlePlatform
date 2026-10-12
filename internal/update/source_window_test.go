package update

import (
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestSourceWindowSecondsCoverSubsecondBounds(t *testing.T) {
	zone := time.FixedZone("source", 8*60*60)
	for _, tc := range []struct {
		name, start, end, wantStart, wantEnd string
	}{
		{"exact", "2026-10-09T04:00:00Z", "2026-10-09T05:00:00Z", "2026-10-09 12:00:00", "2026-10-09 13:00:00"},
		{"fractional", "2026-10-09T04:00:00.123456Z", "2026-10-09T05:00:00.999999Z", "2026-10-09 12:00:00", "2026-10-09 13:00:01"},
		{"midnight", "2026-10-09T15:59:58.999999Z", "2026-10-09T15:59:59.000001Z", "2026-10-09 23:59:58", "2026-10-10 00:00:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, err := time.Parse(time.RFC3339Nano, tc.start)
			if err != nil {
				t.Fatal(err)
			}
			end, err := time.Parse(time.RFC3339Nano, tc.end)
			if err != nil {
				t.Fatal(err)
			}
			w := control.SourceWindow{Start: start, End: end}
			gotStart, gotEnd := sourceWindowTimes(w, zone)
			if gotStart != tc.wantStart || gotEnd != tc.wantEnd {
				t.Fatal(gotStart, gotEnd)
			}
			if !matchesSourceWindow(gotStart, gotEnd, w, zone) {
				t.Fatal("new sealed window rejected")
			}
			legacyStart := start.In(zone).Format("2006-01-02 15:04:05.999999")
			legacyEnd := end.In(zone).Format("2006-01-02 15:04:05.999999")
			if !matchesSourceWindow(legacyStart, legacyEnd, w, zone) {
				t.Fatal("legacy sealed window rejected")
			}
			if gotEnd != legacyEnd && matchesSourceWindow(gotStart, legacyEnd, w, zone) {
				t.Fatal("mixed window formats accepted")
			}
			if matchesSourceWindow("2000-01-01 00:00:00", gotEnd, w, zone) {
				t.Fatal("unrelated window accepted")
			}
		})
	}
}
