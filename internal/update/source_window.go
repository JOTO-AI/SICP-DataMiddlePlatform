package update

import (
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

const sourceWindowLayout = "2006-01-02 15:04:05"

// Source APIs accept whole seconds. Expand the wire window, never shrink it:
// floor the start and ceil the end so subsecond records remain covered.
// The immutable plan and acknowledged progress keep their original precision.
func sourceWindowTimes(w control.SourceWindow, zone *time.Location) (string, string) {
	start := w.Start.Truncate(time.Second)
	end := w.End.Truncate(time.Second)
	if end.Before(w.End) {
		end = end.Add(time.Second)
	}
	return start.In(zone).Format(sourceWindowLayout), end.In(zone).Format(sourceWindowLayout)
}

// Accept an exact legacy pair only for already sealed acquisition/recovery.
// New outbound requests always use sourceWindowTimes.
func matchesSourceWindow(start, end string, w control.SourceWindow, zone *time.Location) bool {
	wantStart, wantEnd := sourceWindowTimes(w, zone)
	if start == wantStart && end == wantEnd {
		return true
	}
	const legacyLayout = "2006-01-02 15:04:05.999999"
	return start == w.Start.In(zone).Format(legacyLayout) && end == w.End.In(zone).Format(legacyLayout)
}
