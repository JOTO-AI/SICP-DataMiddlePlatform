package observability_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type buildReadRecord struct {
	event    events.Event
	body     string
	received time.Time
}
type buildReadFixture struct {
	t                         *testing.T
	records                   []buildReadRecord
	failure                   string
	committed, rolled, closed bool
}
type buildReadRow func(...any) error

func (r buildReadRow) Scan(d ...any) error { return r(d...) }
func (d *buildReadFixture) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	return nil, errors.New("write transaction forbidden")
}
func (d *buildReadFixture) BeginRead(context.Context, storage.Connection) (storage.Transaction, error) {
	if d.failure == "begin" {
		return nil, errors.New("private begin error")
	}
	return d, nil
}
func (d *buildReadFixture) Exec(context.Context, string, ...any) (int64, error) {
	return 0, errors.New("write forbidden")
}
func (d *buildReadFixture) QueryRow(context.Context, string, ...any) storage.Row {
	return buildReadRow(func(v ...any) error {
		*v[0].(*bool) = d.failure != "privilege"
		return nil
	})
}
func (d *buildReadFixture) Commit(context.Context) error {
	if d.failure == "commit" {
		return errors.New("private commit error")
	}
	d.committed = true
	return nil
}
func (d *buildReadFixture) Rollback(context.Context) error { d.rolled = true; return nil }
func (d *buildReadFixture) Query(_ context.Context, q string, a ...any) (storage.Rows, error) {
	if !strings.Contains(q, "received_at<=$4") || !strings.Contains(q, "entity_id>$3") || !strings.Contains(q, "LIMIT $5") || len(a) != 5 {
		d.t.Fatal("unbounded trace page")
	}
	if d.failure == "query" {
		return nil, errors.New("private query error")
	}
	selected := []buildReadRecord{}
	for _, r := range d.records {
		if r.event.Identity.TaskID == a[0] && r.event.Metadata.Build.Stage == a[1] && r.event.EntityID > a[2].(string) && !r.received.After(a[3].(time.Time)) {
			selected = append(selected, r)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].event.EntityID == selected[j].event.EntityID {
			return selected[i].event.Phase < selected[j].event.Phase
		}
		return selected[i].event.EntityID < selected[j].event.EntityID
	})
	ids := map[string]bool{}
	page := []buildReadRecord{}
	for _, r := range selected {
		if !ids[r.event.EntityID] && len(ids) == a[4].(int) {
			break
		}
		ids[r.event.EntityID] = true
		page = append(page, r)
	}
	return &buildReadRows{fixture: d, records: page}, nil
}

type buildReadRows struct {
	fixture *buildReadFixture
	records []buildReadRecord
	pos     int
}

func (r *buildReadRows) Next() bool {
	if r.pos == len(r.records) {
		return false
	}
	r.pos++
	return true
}
func (r *buildReadRows) Close() { r.fixture.closed = true }
func (r *buildReadRows) Err() error {
	if r.fixture.failure == "rows" {
		return errors.New("private cursor error")
	}
	return nil
}
func (r *buildReadRows) Scan(v ...any) error {
	if r.fixture.failure == "scan" {
		return errors.New("private scan error")
	}
	item := r.records[r.pos-1]
	*v[0].(*string), *v[1].(*string), *v[2].(*int), *v[3].(*string) = item.event.EntityID, string(item.event.Phase), len(item.body), item.body
	if r.fixture.failure == "oversize" {
		*v[2].(*int) = 262145
	}
	if r.fixture.failure == "payload" {
		*v[3].(*string) = "{}"
	}
	return nil
}
func buildReaderFixture(t *testing.T) (observability.TaskDiagnostics, *buildReadFixture) {
	t.Helper()
	r, err := logRegistry(t).WithServiceLogAccess(false)
	if err != nil {
		t.Fatal(err)
	}
	d := &buildReadFixture{t: t}
	return observability.TaskDiagnostics{Repository: observability.Repository{Registry: r, Database: d}, Sanitizer: sanitizer(t)}, d
}
func addBuildBoundary(t *testing.T, reader observability.TaskDiagnostics, db *buildReadFixture, task, entity, stage, session, instance string, generation int64, attempt int, phase events.Phase, occurred, received time.Time) {
	t.Helper()
	b := &events.BuildStage{BuildScope: events.BuildScope{Stage: stage, Candidate: "copy-" + instance, Slot: 1, Generation: 7}, SessionID: session, OwnerGeneration: generation, Attempt: attempt}
	e := events.Event{ID: entity + "-" + string(phase), EntityID: entity, Kind: events.Step, Phase: phase, OccurredAt: occurred, Identity: events.Identity{TaskID: task, StepID: entity, Instance: instance}, Metadata: events.Metadata{Build: b}}
	if phase == events.Finished {
		e.Outcome = events.Succeeded
		b.Metrics.RowsWritten = 7
	}
	safe, err := reader.Sanitizer.Sanitize(e, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	db.records = append(db.records, buildReadRecord{safe.Event(), string(safe.Bytes()), received})
}

func TestT31_02And03TraceReaderKeepsCopiesRecoveryAttemptsAndParentsSeparate(t *testing.T) {
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	reader, db := buildReaderFixture(t)
	for _, stage := range []string{"write_copy", "route_shift", "drain", "publish", "confirm_progress"} {
		for _, instance := range []string{"S", "M"} {
			for attempt := 1; attempt <= 2; attempt++ {
				entity := fmt.Sprintf("%s-%s-%d", stage, instance, attempt)
				for _, phase := range []events.Phase{events.Started, events.Finished} {
					addBuildBoundary(t, reader, db, "task", entity, stage, fmt.Sprintf("session-%d", attempt), instance, int64(attempt), attempt, phase, at, at)
				}
			}
		}
	}
	addBuildBoundary(t, reader, db, "rerun", "other-task", "write_copy", "session-1", "M", 1, 1, events.Finished, at, at)
	for _, stage := range []string{"write_copy", "route_shift", "drain", "publish", "confirm_progress"} {
		after := ""
		seen := map[string]bool{}
		for page := 0; page < 3; page++ {
			got, err := reader.BuildOperations(t.Context(), "task", stage, after, at, 2)
			if err != nil {
				t.Fatal(err)
			}
			if page == 2 && len(got) != 0 || page < 2 && len(got) != 2 {
				t.Fatal("unstable page", page, len(got))
			}
			for _, v := range got {
				if seen[v.ID] || v.ID <= after || v.Scope.Stage != stage || v.State != "succeeded" || v.StartedAt == nil || v.FinishedAt == nil || v.DurationNS != nil || v.Metrics == nil || v.Metrics.RowsWritten != 7 || v.OwnerGeneration != int64(v.Attempt) || v.SessionID != reader.Sanitizer.Correlation(fmt.Sprintf("session-%d", v.Attempt)) {
					t.Fatal("mixed copy/session, fabricated duration or parent", v)
				}
				seen[v.ID] = true
				after = v.ID
			}
		}
		if len(seen) != 4 {
			t.Fatal("lost copy or attempt")
		}
	}
}

func TestT31_03LateAndMissingBoundariesRemainUnknownUntilFreshRead(t *testing.T) {
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	reader, db := buildReaderFixture(t)
	addBuildBoundary(t, reader, db, "task", "step", "drain", "session", "M", 1, 1, events.Started, at, at)
	addBuildBoundary(t, reader, db, "task", "step", "drain", "session", "M", 1, 1, events.Finished, at.Add(time.Second), at.Add(time.Minute))
	for _, fresh := range []bool{false, true} {
		cutoff := at
		if fresh {
			cutoff = at.Add(time.Minute)
		}
		got, err := reader.BuildOperations(t.Context(), "task", "drain", "", cutoff, 2)
		if err != nil || len(got) != 1 {
			t.Fatal(got, err)
		}
		v := got[0]
		if v.ID != db.records[0].event.EntityID || v.StartedAt == nil || !fresh && (v.State != "unknown" || v.FinishedAt != nil || v.Metrics != nil) || fresh && (v.State != "succeeded" || v.FinishedAt == nil) {
			t.Fatal("invented or lost boundary", v)
		}
	}
	db.records = db.records[1:]
	got, err := reader.BuildOperations(t.Context(), "task", "drain", "", at.Add(time.Minute), 2)
	if err != nil || len(got) != 1 || got[0].StartedAt != nil || got[0].DurationNS != nil {
		t.Fatal("finish invented start or duration", got, err)
	}
}

func TestT31_03TraceReaderFailuresNeverReturnPartialSuccess(t *testing.T) {
	for _, failure := range []string{"begin", "privilege", "query", "scan", "rows", "oversize", "payload", "commit", "session_conflict", "duplicate"} {
		t.Run(failure, func(t *testing.T) {
			at := time.Now().UTC()
			reader, db := buildReaderFixture(t)
			db.failure = failure
			addBuildBoundary(t, reader, db, "task", "step", "write_copy", "session", "M", 1, 1, events.Started, at, at)
			if failure == "session_conflict" {
				addBuildBoundary(t, reader, db, "task", "step", "write_copy", "other-session", "M", 2, 1, events.Finished, at, at)
			}
			if failure == "duplicate" {
				db.records = append(db.records, db.records[0])
			}
			got, err := reader.BuildOperations(t.Context(), "task", "write_copy", "", at, 2)
			if err == nil || got != nil || db.committed || failure != "begin" && !db.rolled {
				t.Fatal("partial success", got, err)
			}
		})
	}
}
