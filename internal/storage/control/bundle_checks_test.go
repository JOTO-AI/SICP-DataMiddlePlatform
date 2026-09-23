package control_test

import (
	"strings"
	"testing"
	"time"
)

func TestT11_18BundleChecksUseOnePublishedBundleStatementAndNoPartialRead(t *testing.T) {
	r, d := repository(t)
	d.raw = []any{`{"dwd_bzkscjzlb":"2026-09-18T01:00:00Z"}`}
	checks, err := r.BundleChecks(t.Context(), "old-C")
	if err != nil || !checks["dwd_bzkscjzlb"].Equal(time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC)) {
		t.Fatal(checks, err)
	}
	if len(d.calls) != 1 || d.calls[0].args[0] != "old-C" || !strings.Contains(d.calls[0].sql, "published_at IS NOT NULL") || !strings.Contains(d.calls[0].sql, "jsonb_object_agg") {
		t.Fatal(d.calls)
	}
	d.failCommit = true
	if checks, err = r.BundleChecks(t.Context(), "old-C"); err == nil || checks != nil {
		t.Fatal(checks, err)
	}
}
