package control_test

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type progressDB struct {
	base      *publicationDB
	task      control.Task
	plan      string
	progress  map[string]control.Progress
	checks    map[string]time.Time
	published *time.Time
	open      bool
	failAt    int
}
type progressTx struct {
	*publicationTx
	db        *progressDB
	task      control.Task
	plan      string
	progress  map[string]control.Progress
	checks    map[string]time.Time
	published *time.Time
	open      bool
	writes    int
}

func (d *progressDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	base, err := d.base.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return &progressTx{publicationTx: base.(*publicationTx), db: d, task: d.task, plan: d.plan, progress: maps.Clone(d.progress), checks: maps.Clone(d.checks), published: d.published, open: d.open}, nil
}
func (t *progressTx) QueryRow(ctx context.Context, sql string, args ...any) storage.Row {
	switch {
	case strings.HasPrefix(sql, "SELECT payload::text FROM control.checkpoints"):
		if t.plan == "" {
			return result{raw: []any{""}, fail: true}
		}
		return result{raw: []any{t.plan}}
	case strings.Contains(sql, `FROM control.tasks WHERE "id"=$1`):
		return result{raw: entityColumns(t.task)}
	default:
		return t.publicationTx.QueryRow(ctx, sql, args...)
	}
}
func (t *progressTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	switch {
	case strings.HasPrefix(sql, "INSERT INTO control.checkpoints"):
		if t.plan != "" {
			return 0, nil
		}
		t.plan = args[2].(string)
	case strings.HasPrefix(sql, "UPDATE control.bundles SET published_at="):
		at := args[1].(time.Time)
		t.published = &at
	case strings.HasPrefix(sql, "UPDATE control.slots s SET admission_open="):
		t.open = true
		return 2, nil
	case strings.HasPrefix(sql, "UPDATE control.tasks SET published_at="):
		at := args[1].(time.Time)
		t.task.PublishedAt = &at
		t.task.Status = "published_pending_progress"
	case strings.HasPrefix(sql, "UPDATE control.publication_outbox SET status='confirmed'"):
		e := t.events[args[0].(string)]
		at := args[1].(time.Time)
		e.Status = "confirmed"
		e.ConfirmedAt = &at
		t.events[e.ID] = e
	case strings.HasPrefix(sql, "INSERT INTO control.source_progress"):
		t.writes++
		if t.db.failAt == t.writes {
			return 0, errors.New("synthetic progress write failure")
		}
		p := control.Progress{Source: args[0].(string), ConfirmedUntil: args[1].(time.Time), CheckedAt: args[2].(time.Time), TaskID: args[3].(string)}
		if old, ok := t.progress[p.Source]; ok && (old.ConfirmedUntil.After(p.ConfirmedUntil) || old.CheckedAt.After(p.CheckedAt)) {
			return 0, nil
		}
		t.progress[p.Source] = p
	case strings.HasPrefix(sql, "INSERT INTO control.bundle_checks"):
		t.checks[args[1].(string)] = args[2].(time.Time)
	case strings.HasPrefix(sql, "UPDATE control.bundles SET checked_at="):
	case strings.HasPrefix(sql, "UPDATE control.tasks SET status='success_published'"):
		t.task.Status = "success_published"
		checked, finished := args[1].(time.Time), args[2].(time.Time)
		t.task.CheckedAt = &checked
		t.task.FinishedAt = &finished
	default:
		return t.publicationTx.Exec(ctx, sql, args...)
	}
	return 1, nil
}
func (t *progressTx) Commit(ctx context.Context) error {
	if err := t.publicationTx.Commit(ctx); err != nil {
		return err
	}
	d := t.db
	d.task = t.task
	d.plan = t.plan
	d.progress = t.progress
	d.checks = t.checks
	d.published = t.published
	d.open = t.open
	if d.open {
		d.base.slot.AdmissionOpen = true
	}
	if d.published != nil {
		d.base.unpublished = false
	}
	return nil
}
func progressFixture(t *testing.T) (control.Repository, *progressDB, control.ProgressPlan, control.PublicationIntent) {
	t.Helper()
	r, base, intent := publicationFixture(t)
	start := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	intent.CreatedAt = start.Add(25 * time.Minute)
	d := &progressDB{base: base, task: control.Task{ID: "task-A", Mode: "initialize", Status: "running", StartedAt: &start}, progress: map[string]control.Progress{}, checks: map[string]time.Time{}}
	r.Database = d
	p := control.ProgressPlan{TaskID: "task-A", BundleID: "C101", Mode: "initialize", StartedAt: start, CheckedAt: start.Add(25 * time.Minute), Ends: map[string]time.Time{}}
	for _, table := range source.Catalog() {
		p.Ends[table.Name] = start
	}
	return r, d, p, intent
}
func appliedEvent(t *testing.T, r control.Repository, p control.ProgressPlan, i control.PublicationIntent) control.Publication {
	t.Helper()
	if err := r.StageProgress(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	e, _, err := r.EnqueuePublication(t.Context(), i)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RecordPublicationApplied(t.Context(), e, p.StartedAt.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return e
}
func TestT12_13StartedAtProgressAndAcknowledgedPublicationAreSeparateDurableFacts(t *testing.T) {
	r, d, p, i := progressFixture(t)
	e := appliedEvent(t, r, p, i)
	at := p.StartedAt.Add(30 * time.Minute)
	if len(d.progress) != 0 || d.published != nil {
		t.Fatal("application prematurely published or advanced progress")
	}
	e, err := r.ConfirmPublication(t.Context(), e, at)
	if err != nil {
		t.Fatal(err)
	}
	if d.published == nil || !d.published.Equal(at) || !d.open || d.task.Status != "published_pending_progress" || len(d.progress) != 0 || len(d.checks) != 14 {
		t.Fatal("formal publication mixed with progress")
	}
	if err = r.CompletePublished(t.Context(), e, at); err != nil {
		t.Fatal(err)
	}
	if len(d.progress) != 14 || len(d.checks) != 14 || d.task.Status != "success_published" {
		t.Fatal("complete progress not atomic")
	}
	for _, progress := range d.progress {
		if !progress.ConfirmedUntil.Equal(p.StartedAt) || !progress.ConfirmedUntil.Add(-control.IncrementalOverlap).Equal(p.StartedAt.Add(-5*time.Minute)) {
			t.Fatal("08:30 publication replaced 08:00 progress/07:55 next start")
		}
	}
	if err = r.CompletePublished(t.Context(), e, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !d.task.FinishedAt.Equal(at) {
		t.Fatal("idempotent confirmation changed original success time")
	}
}
func TestT12_15ProgressFailureKeepsPublishedCopyAndRetriesOnlyControlTransaction(t *testing.T) {
	for _, failure := range []string{"write-seven", "commit", "backwards"} {
		t.Run(failure, func(t *testing.T) {
			r, d, p, i := progressFixture(t)
			e := appliedEvent(t, r, p, i)
			at := p.StartedAt.Add(30 * time.Minute)
			e, err := r.ConfirmPublication(t.Context(), e, at)
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "write-seven":
				d.failAt = 7
			case "commit":
				d.base.failCommit = true
			case "backwards":
				name := source.Catalog()[0].Name
				d.progress[name] = control.Progress{Source: name, ConfirmedUntil: at, CheckedAt: at}
			}
			before := maps.Clone(d.progress)
			beforeChecks := maps.Clone(d.checks)
			if err = r.CompletePublished(t.Context(), e, at); err == nil {
				t.Fatal("progress failure accepted")
			}
			if !maps.Equal(d.progress, before) || !maps.Equal(d.checks, beforeChecks) || d.task.Status != "published_pending_progress" || d.published == nil || !d.open || d.base.events[e.ID].Status != "confirmed" {
				t.Fatal("progress rollback damaged publication or partially advanced sources")
			}
			if failure == "backwards" {
				return
			}
			d.failAt = 0
			d.base.failCommit = false
			if err = r.CompletePublished(t.Context(), e, at.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if len(d.progress) != 14 || len(d.checks) != 14 || d.task.Status != "success_published" {
				t.Fatal("progress-only retry incomplete")
			}
		})
	}
}
func TestT12_07UnacknowledgedOrUncommittedPublicationNeverOpensOrAdvances(t *testing.T) {
	for _, failure := range []string{"no-application", "confirmation-commit"} {
		t.Run(failure, func(t *testing.T) {
			r, d, p, i := progressFixture(t)
			if err := r.StageProgress(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			e, _, err := r.EnqueuePublication(t.Context(), i)
			if err != nil {
				t.Fatal(err)
			}
			at := p.StartedAt.Add(30 * time.Minute)
			if failure == "confirmation-commit" {
				if err = r.RecordPublicationApplied(t.Context(), e, at); err != nil {
					t.Fatal(err)
				}
				d.base.failCommit = true
			}
			if _, err = r.ConfirmPublication(t.Context(), e, at); err == nil {
				t.Fatal("unconfirmed publication accepted")
			}
			if d.open || d.published != nil || len(d.progress) != 0 || len(d.checks) != 0 || d.base.events[e.ID].Status != "pending" {
				t.Fatal("failed confirmation advanced facts")
			}
			d.base.failCommit = false
			if err = r.CompletePublished(t.Context(), e, at); err == nil {
				t.Fatal("pending event advanced success")
			}
		})
	}
}
func TestT12_13ProgressPlanRequiresFourteenFixedBoundsAndImmutableTaskStart(t *testing.T) {
	for _, failure := range []string{"missing-source", "publication-as-end", "wrong-start", "wrong-mode"} {
		t.Run(failure, func(t *testing.T) {
			r, d, p, _ := progressFixture(t)
			switch failure {
			case "missing-source":
				delete(p.Ends, source.Catalog()[0].Name)
			case "publication-as-end":
				p.Ends[source.Catalog()[0].Name] = p.CheckedAt
			case "wrong-start":
				p.StartedAt = p.StartedAt.Add(time.Minute)
				for name := range p.Ends {
					p.Ends[name] = p.StartedAt
				}
			case "wrong-mode":
				p.Mode = "incremental"
			}
			if err := r.StageProgress(t.Context(), p); err == nil || d.plan != "" {
				t.Fatal("invalid progress plan persisted")
			}
		})
	}
	r, _, p, _ := progressFixture(t)
	if err := r.StageProgress(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if err := r.StageProgress(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	p.CheckedAt = p.CheckedAt.Add(time.Second)
	if err := r.StageProgress(t.Context(), p); !errors.Is(err, control.ErrIdempotency) {
		t.Fatal("immutable plan overwritten", err)
	}
}

func TestT12_15ActualFinisherReplaysConfirmedEventWithoutAnotherRedisWrite(t *testing.T) {
	r, d, p, i := progressFixture(t)
	if err := r.StageProgress(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	e, _, err := r.EnqueuePublication(t.Context(), i)
	if err != nil {
		t.Fatal(err)
	}
	at := p.StartedAt.Add(30 * time.Minute)
	redis := &deliveryRedis{}
	f := update.Finisher{Store: r, Delivery: deliverer(r, redis, at), Clock: testkit.FakeClock{Time: at}}
	d.failAt = 7
	if err = f.Finish(t.Context(), e); err == nil {
		t.Fatal("partial progress accepted")
	}
	if redis.writes != 1 || d.base.events[e.ID].Status != "confirmed" || len(d.progress) != 0 || len(d.checks) != 14 {
		t.Fatal("bad pending-progress state")
	}
	calls := redis.calls
	d.failAt = 0
	if err = f.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if redis.calls != calls || redis.writes != 1 || len(d.progress) != 14 || d.task.Status != "success_published" {
		t.Fatal("control-only recovery redelivered active metadata")
	}
}
