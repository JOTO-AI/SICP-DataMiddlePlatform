package control_test

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type planDB struct {
	base     *publicationDB
	task     control.Task
	progress map[string]time.Time
	writes   int
	table    string
}
type planTx struct {
	*publicationTx
	db   *planDB
	task control.Task
}

func (d *planDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	t, err := d.base.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return &planTx{publicationTx: t.(*publicationTx), db: d, task: d.task}, nil
}
func (t *planTx) QueryRow(ctx context.Context, sql string, args ...any) storage.Row {
	switch {
	case strings.Contains(sql, "name='table-selection'"):
		return result{raw: []any{t.db.table}}
	case strings.Contains(sql, `FROM control.tasks WHERE "id"=$1`):
		return result{raw: entityColumns(t.task)}
	case strings.HasPrefix(sql, "SELECT confirmed_until FROM control.source_progress"):
		until, ok := t.db.progress[args[0].(string)]
		return result{raw: []any{until}, fail: !ok}
	default:
		return t.publicationTx.QueryRow(ctx, sql, args...)
	}
}
func (t *planTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if strings.HasPrefix(sql, "UPDATE control.tasks SET roles=") {
		t.db.writes++
		doc := control.Document(args[1].(string))
		t.task.Roles = &doc
		return 1, nil
	}
	return t.publicationTx.Exec(ctx, sql, args...)
}
func (t *planTx) Commit(ctx context.Context) error {
	if err := t.publicationTx.Commit(ctx); err != nil {
		return err
	}
	t.db.task = t.task
	return nil
}
func planFixture(t *testing.T) (control.Repository, *planDB, control.ActiveMetadata) {
	t.Helper()
	r, base, _ := publicationFixture(t)
	start := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	a := control.ActiveMetadata{Sequence: 1, EventID: "initial", TaskID: "previous", OwnerGeneration: 1, Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 1, BundleID: "C100"}}
	doc, err := control.ActiveDocument(a)
	if err != nil {
		t.Fatal(err)
	}
	base.confirmed = &doc
	d := &planDB{base: base, task: control.Task{ID: "task-A", Mode: "incremental", Status: "running", StartedAt: &start}, progress: map[string]time.Time{}}
	for _, table := range source.Catalog() {
		d.progress[table.Name] = start.Add(-2 * time.Hour)
	}
	r.Database = d
	return r, d, a
}
func TestT13_01FixedFourteenWindowsUseSuccessfulProgressAndFiveMinuteOverlap(t *testing.T) {
	r, d, a := planFixture(t)
	p, err := r.PlanUpdate(t.Context(), a, 0)
	if err != nil || p.Validate() != nil || len(p.Windows) != 14 || d.task.Roles == nil {
		t.Fatal(p, err)
	}
	for _, w := range p.Windows {
		if w.Start.Format("15:04") != "07:55" || w.End.Format("15:04") != "10:00" {
			t.Fatal(w)
		}
	}
	// Caller mutations cannot change the stored plan; retries load original bounds.
	p.Windows[source.Catalog()[0].Name] = control.SourceWindow{}
	q, err := r.PlanUpdate(t.Context(), a, time.Minute)
	if err != nil || q.Validate() != nil || q.Overlap != 5*time.Minute || d.writes != 1 {
		t.Fatal(q, err)
	}
}
func TestT13_02EachTableRetainsItsOwnLastSuccessfulPoint(t *testing.T) {
	r, d, a := planFixture(t)
	name := source.Catalog()[4].Name
	d.progress[name] = d.progress[name].Add(-time.Hour)
	p, err := r.PlanUpdate(t.Context(), a, 0)
	if err != nil || p.Windows[name].Start.Format("15:04") != "06:55" || p.Windows[source.Catalog()[0].Name].Start.Format("15:04") != "07:55" {
		t.Fatal(p, err)
	}
}
func TestT13_08PersistedDirectionSurvivesRouteShiftAndRepositoryRecreation(t *testing.T) {
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		t.Run(string(instance), func(t *testing.T) {
			r, d, a := planFixture(t)
			a.Position.Position.Instance = instance
			doc, _ := control.ActiveDocument(a)
			d.base.confirmed = &doc
			p, err := r.PlanUpdate(t.Context(), a, 0)
			if err != nil {
				t.Fatal(err)
			}
			shifted := a
			shifted.Position.Position.Instance = p.First
			shifted.Sequence++
			shifted.EventID = "shift"
			doc, _ = control.ActiveDocument(shifted)
			d.base.confirmed = &doc
			restarted := control.Repository{Registry: r.Registry, Database: d}.WithOwnership(control.Ownership{TaskID: "task-A", OwnerID: "owner-A", Generation: 9})
			q, err := restarted.PlanUpdate(t.Context(), shifted, 0)
			if err != nil || !reflect.DeepEqual(p, q) || q.Second != instance || q.First == instance || d.writes != 1 {
				t.Fatal(q, err)
			}
		})
	}
}
func TestT13_18ForceFullClockRollbackRejectedBeforeAcquisition(t *testing.T) {
	r, d, a := planFixture(t)
	d.task.Mode = "force_full"
	for name := range d.progress {
		d.progress[name] = *d.task.StartedAt
	}
	clock := testkit.FakeClock{Time: d.task.StartedAt.Add(-time.Minute)}
	at := clock.Now()
	d.task.StartedAt = &at
	before := maps.Clone(d.progress)
	p, err := r.PlanUpdate(t.Context(), a, 0)
	if !errors.Is(err, control.ErrTime) || p.TaskID != "" || d.writes != 0 || d.task.Roles != nil || !maps.Equal(before, d.progress) || len(d.base.events) != 0 {
		t.Fatal(p, err)
	}
}
func TestT13_19OverlapDoesNotHideOneTableFutureProgress(t *testing.T) {
	r, d, a := planFixture(t)
	for name := range d.progress {
		d.progress[name] = d.task.StartedAt.Add(-10 * time.Minute)
	}
	d.progress[source.Catalog()[13].Name] = *d.task.StartedAt
	at := d.task.StartedAt.Add(-time.Minute)
	d.task.StartedAt = &at
	p, err := r.PlanUpdate(t.Context(), a, 0)
	if !errors.Is(err, control.ErrTime) || p.TaskID != "" || d.writes != 0 || d.task.Roles != nil {
		t.Fatal(p, err)
	}
}
func TestT13_01PlanningFailuresReturnNoPlanOrPartialRoles(t *testing.T) {
	for _, kind := range []string{"commit", "missing-progress", "pending", "wrong-route", "owner", "mode", "unstarted", "invalid-overlap"} {
		t.Run(kind, func(t *testing.T) {
			r, d, a := planFixture(t)
			overlap := time.Duration(0)
			switch kind {
			case "commit":
				d.base.failCommit = true
			case "missing-progress":
				delete(d.progress, source.Catalog()[5].Name)
			case "pending":
				d.base.events["pending"] = control.Publication{Status: "pending"}
			case "wrong-route":
				a.Sequence++
			case "owner":
				r = r.WithOwnership(control.Ownership{TaskID: "task-A", OwnerID: "stale", Generation: 8})
			case "mode":
				d.task.Mode = "initialize"
			case "unstarted":
				d.task.StartedAt = nil
			case "invalid-overlap":
				overlap = -time.Minute
			}
			p, err := r.PlanUpdate(t.Context(), a, overlap)
			if err == nil || p.TaskID != "" || d.task.Roles != nil {
				t.Fatal(p, err)
			}
		})
	}
}
