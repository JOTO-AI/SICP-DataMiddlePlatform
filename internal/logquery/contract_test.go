package logquery_test

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
)

func instant() time.Time { return time.Date(2026, 10, 12, 4, 0, 0, 0, time.UTC) }

func parseFilter(values url.Values, now time.Time, base *logquery.Filter) (logquery.Filter, string, error) {
	ids, err := observability.NewSanitizer(4096, 1024, bytes.Repeat([]byte{3}, 32), nil)
	if err != nil {
		panic(err)
	}
	return logquery.ParseFilter(values, now, base, ids)
}

func TestT27_FilterDefaultsAndContinuationKeepOriginalWindow(t *testing.T) {
	f, cursor, err := parseFilter(url.Values{}, instant(), nil)
	if err != nil || cursor != "" || f.PageSize != 50 || !f.From.Equal(instant().Add(-24*time.Hour)) || !f.To.Equal(instant()) {
		t.Fatalf("wrong default filter: %#v %v", f, err)
	}
	again, _, err := parseFilter(url.Values{"cursor": {"authenticated-by-caller"}}, instant().Add(time.Hour), &f)
	if err != nil || again != f {
		t.Fatal("continuation shifted the default time range", err)
	}
	zoned, _, err := parseFilter(url.Values{"from": {"2026-10-11T12:00:00+08:00"}, "to": {"2026-10-12T12:00:00+08:00"}}, instant(), nil)
	if err != nil || zoned != f {
		t.Fatal("equivalent timezone not canonicalized", err)
	}
	until, _, err := parseFilter(url.Values{"to": {"2026-10-10T12:00:00+08:00"}}, instant(), nil)
	if err != nil || until.To.Sub(until.From) != 24*time.Hour {
		t.Fatal("to-only default did not follow supplied end", err)
	}
}

func TestT27_FilterRejectsAmbiguousAndUnboundedInput(t *testing.T) {
	for name, values := range map[string]url.Values{
		"unknown":             {"sql": {"select"}},
		"duplicate":           {"kind": {"request", "task"}},
		"empty":               {"outcome": {""}},
		"nil-values":          {"kind": nil},
		"kind":                {"kind": {"audit-unsupported"}},
		"phase":               {"phase": {"unknown"}},
		"outcome":             {"outcome": {"pending"}},
		"operation":           {"operation": {"unknown"}},
		"identity":            {"task_id": {"id' OR 1=1"}},
		"identity-length":     {"request_id": {strings.Repeat("x", 129)}},
		"page-zero":           {"page_size": {"0"}},
		"page-large":          {"page_size": {"201"}},
		"page-noncanonical":   {"page_size": {"050"}},
		"page-sign":           {"page_size": {"+50"}},
		"time-zone-missing":   {"from": {"2026-10-11T04:00:00"}},
		"time-submicrosecond": {"from": {"2026-10-11T04:00:00.000000001Z"}},
		"time-invalid-day":    {"from": {"2026-02-30T04:00:00Z"}},
		"reverse":             {"from": {"2026-10-13T04:00:00Z"}},
		"range":               {"from": {"2026-10-01T04:00:00Z"}},
		"cursor-limit":        {"cursor": {strings.Repeat("x", logquery.MaxCursorBytes+1)}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseFilter(values, instant(), nil); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestT27_FilterAcceptsInclusiveBudgetAndKnownEnums(t *testing.T) {
	f, _, err := parseFilter(url.Values{"from": {"2026-10-05T04:00:00Z"}, "page_size": {"200"}, "kind": {"attempt"}, "phase": {"finished"}, "outcome": {"failed"}, "task_id": {"task_001-A"}}, instant(), nil)
	if err != nil || f.To.Sub(f.From) != logquery.MaxRange || f.PageSize != 200 {
		t.Fatal("valid boundary rejected", err)
	}
	if !logquery.Platform.Valid() || !logquery.Upstream.Valid() || logquery.Source("database-name").Valid() {
		t.Fatal("log source is not a fixed allowlist")
	}
}
