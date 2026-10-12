package observability_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type dualReadDB struct {
	sources map[storage.Connection]*logsDB
	fail    storage.Connection
}

func (d *dualReadDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	if c == d.fail {
		return nil, errors.New("private-driver-error")
	}
	db := d.sources[c]
	if db == nil {
		return nil, errors.New("unexpected target")
	}
	return db.Begin(ctx, c)
}

func diagnosticFixture(t *testing.T, count int) (observability.TaskDiagnostics, *dualReadDB) {
	t.Helper()
	r := logRegistry(t, true)
	u, _ := r.LogConnection(storage.UpstreamLog)
	s := sanitizer(t)
	db := &dualReadDB{sources: map[storage.Connection]*logsDB{r.Logs(): {rows: map[string]string{}}, u: {rows: map[string]string{}}}}
	for _, target := range []storage.Connection{r.Logs(), u} {
		for i := 0; i < count; i++ {
			e := event()
			e.ID = fmt.Sprintf("shared-id-%d", i)
			e.Kind = events.Attempt
			e.Phase = events.Finished
			e.Outcome = events.Failed
			e.Identity = events.Identity{TaskID: "task", AttemptNo: i + 1}
			e.OccurredAt = e.OccurredAt.Add(time.Duration(i) * time.Second)
			if target == u {
				e.OccurredAt = e.OccurredAt.Add(time.Millisecond)
			}
			safe, err := s.Sanitize(e, observability.Details{})
			if err != nil {
				t.Fatal(err)
			}
			db.sources[target].rows[safe.ID()] = string(safe.Bytes())
		}
	}
	return observability.TaskDiagnostics{Repository: observability.Repository{Registry: r, Database: db}, Sanitizer: s}, db
}

func TestT28_12DualDiagnosticsRetainOriginAndBoundMerge(t *testing.T) {
	d, _ := diagnosticFixture(t, 20)
	report, err := d.FailedAttemptReport(t.Context(), "task")
	if err != nil || !report.Complete || len(report.Items) != 20 || len(report.Sources) != 2 {
		t.Fatal("bounded merge failed", err)
	}
	seen := map[string]bool{}
	for i, row := range report.Items {
		key := string(row.LogSource) + row.EventID
		if seen[key] {
			t.Fatal("duplicate summary")
		}
		seen[key] = true
		if i > 0 && row.At.After(report.Items[i-1].At) {
			t.Fatal("unstable time order")
		}
	}
	if report.Items[0].EventID != report.Items[1].EventID || report.Items[0].LogSource == report.Items[1].LogSource {
		t.Fatal("cross-source IDs conflated")
	}
}

func TestT28_13UnavailableSourceIsExplicitAndHealthyEvidenceRemains(t *testing.T) {
	d, db := diagnosticFixture(t, 2)
	u, _ := d.Repository.Registry.LogConnection(storage.UpstreamLog)
	db.fail = u
	report, err := d.FailedAttemptReport(t.Context(), "task")
	if err != nil || report.Complete || len(report.Items) != 2 || report.Sources[1].Complete || report.Sources[1].Reason != "unavailable" {
		t.Fatal("unavailable source became empty success")
	}
	if items, err := d.FailedAttempts(t.Context(), "task"); err == nil || items != nil {
		t.Fatal("legacy diagnostic API hid partial report")
	}
	db.fail = storage.Connection{}
	report, err = d.FailedAttemptReport(t.Context(), "task")
	if err != nil || !report.Complete || len(report.Items) != 4 {
		t.Fatal("recovery lost evidence")
	}
	report, err = d.FailedAttemptReport(t.Context(), "other-task")
	if err != nil || !report.Complete || len(report.Items) != 0 {
		t.Fatal("verified empty is not represented")
	}
}
