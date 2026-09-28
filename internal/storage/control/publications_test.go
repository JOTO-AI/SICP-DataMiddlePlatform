package control_test

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type publicationDB struct {
	events             map[string]control.Publication
	sequence           int64
	commits, rollbacks int
	failCommit         bool
	ready, unpublished bool
	copies             int
	slot               control.Slot
	confirmed          *control.Document
	calls              []dbCall
}
type publicationTx struct {
	d         *publicationDB
	events    map[string]control.Publication
	committed bool
}

func (d *publicationDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	return &publicationTx{d: d, events: maps.Clone(d.events)}, nil
}
func (t *publicationTx) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	t.d.calls = append(t.d.calls, dbCall{sql, args})
	if strings.HasPrefix(sql, "UPDATE control.publication_outbox SET target=") {
		e := t.events[args[0].(string)]
		e.Target = control.Document(args[1].(string))
		t.events[e.ID] = e
	}
	if strings.HasPrefix(sql, "UPDATE control.publication_outbox SET applied_at=") {
		e := t.events[args[0].(string)]
		at := args[1].(time.Time)
		e.AppliedAt = &at
		t.events[e.ID] = e
	}
	return 1, nil
}
func entityColumns(v any) []any {
	r := reflect.ValueOf(v)
	out := make([]any, r.NumField())
	for i := range out {
		f := r.Field(i)
		if f.Kind() == reflect.Pointer {
			if f.IsNil() {
				continue
			}
			f = f.Elem()
		}
		out[i] = f.Interface()
	}
	return out
}
func (t *publicationTx) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	t.d.calls = append(t.d.calls, dbCall{sql, args})
	switch {
	case strings.HasPrefix(sql, "SELECT task_id,owner_id,generation"):
		return result{raw: []any{"task-A", "owner-A", int64(9)}}
	case strings.Contains(sql, "publication_outbox WHERE id=$1"):
		_, ok := t.events[args[0].(string)]
		return result{raw: []any{ok}}
	case strings.Contains(sql, `FROM control.publication_outbox WHERE "id"=$1`):
		return result{raw: entityColumns(t.events[args[0].(string)])}
	case strings.Contains(sql, "publication_outbox WHERE status='pending'"):
		pending := false
		for _, e := range t.events {
			pending = pending || e.Status == "pending"
		}
		return result{raw: []any{pending}}
	case strings.HasPrefix(sql, "SELECT (SELECT target::text"):
		if t.d.confirmed == nil {
			return result{raw: []any{nil}}
		}
		return result{raw: []any{string(*t.d.confirmed)}}
	case strings.HasPrefix(sql, "SELECT generation,bundle_id,admission_open,state"):
		return result{raw: []any{t.d.slot.Generation, *t.d.slot.BundleID, t.d.slot.AdmissionOpen, t.d.slot.State}}
	case strings.Contains(sql, "SELECT 1 FROM control.candidates"):
		return result{raw: []any{t.d.ready}}
	case strings.Contains(sql, "published_at IS NULL"):
		return result{raw: []any{t.d.unpublished}}
	case strings.Contains(sql, "published_at IS NOT NULL"):
		return result{raw: []any{!t.d.unpublished}}
	case strings.HasPrefix(sql, "SELECT COUNT(DISTINCT"):
		return result{raw: []any{t.d.copies}}
	case strings.HasPrefix(sql, "INSERT INTO control.publication_outbox"):
		t.d.sequence++
		e := control.Publication{ID: args[0].(string), Sequence: t.d.sequence, TaskID: args[1].(string), Kind: args[2].(string), Target: control.Document(args[4].(string)), Status: "pending", CreatedAt: args[5].(time.Time)}
		if args[3] != nil {
			doc := control.Document(args[3].(string))
			e.Expected = &doc
		}
		t.events[e.ID] = e
		return result{raw: []any{e.Sequence}}
	default:
		panic("unexpected publication SQL: " + sql)
	}
}
func (t *publicationTx) Commit(context.Context) error {
	if t.d.failCommit {
		return errors.New("unknown commit result")
	}
	t.d.events = t.events
	t.d.commits++
	t.committed = true
	return nil
}
func (t *publicationTx) Rollback(context.Context) error {
	if !t.committed {
		t.d.rollbacks++
	}
	return nil
}

func publicationFixture(t *testing.T) (control.Repository, *publicationDB, control.PublicationIntent) {
	t.Helper()
	r, _ := repository(t)
	bundle := "C101"
	d := &publicationDB{events: map[string]control.Publication{}, sequence: 9007199254740992, ready: true, unpublished: true, copies: 2, slot: control.Slot{Instance: "S", Slot: 1, Generation: 8, BundleID: &bundle, State: "ready"}}
	r.Database = d
	i := control.PublicationIntent{ID: "publish-1", Kind: "bundle_publish", Target: control.SlotIdentity{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 8, BundleID: bundle}, CreatedAt: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}
	return writer(r), d, i
}

type deliveryFunc func(context.Context, control.Publication) error

func (f deliveryFunc) Apply(ctx context.Context, e control.Publication) error { return f(ctx, e) }

func TestT12_16ActualOutboxPersistsFullIntentExactlyOnceBeforeDelivery(t *testing.T) {
	r, d, i := publicationFixture(t)
	calls := 0
	delivery := deliveryFunc(func(_ context.Context, e control.Publication) error {
		calls++
		stored, ok := d.events[e.ID]
		if !ok || stored != e || d.commits < 1 {
			t.Fatal("network before durable intent", e)
		}
		return nil
	})
	e, err := publication.Submit(t.Context(), r, delivery, i)
	if err != nil || e.Sequence != 9007199254740993 || e.Status != "pending" || len(d.events) != 1 {
		t.Fatal(e, err)
	}
	target, err := control.DecodeActive(e.Target)
	if err != nil || target.Position != i.Target || target.Sequence != e.Sequence || target.OwnerGeneration != 9 || target.TaskID != "task-A" || !strings.Contains(string(e.Target), `"sequence":"9007199254740993"`) {
		t.Fatal(target, err, e.Target)
	}
	replayed, created, err := r.EnqueuePublication(t.Context(), i)
	if err != nil || created || replayed != e || len(d.events) != 1 || d.sequence != e.Sequence || calls != 1 {
		t.Fatal(replayed, created, err)
	}
	i.Target.Generation++
	if _, _, err = r.EnqueuePublication(t.Context(), i); !errors.Is(err, control.ErrIdempotency) {
		t.Fatal("event identity overwritten", err)
	}
}

func TestLegacyPublicationReplaysWithoutCodeVersionConfiguration(t *testing.T) {
	r, d, intent := publicationFixture(t)
	event, _, err := r.EnqueuePublication(t.Context(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(event.Target), "code_version") {
		t.Fatal("new publication contains code version", event.Target)
	}
	// Simulate an immutable outbox event created before CODE_VERSION was removed.
	event.Target = control.Document(strings.TrimSuffix(string(event.Target), "}") + `,"code_version":"previous-release"}`)
	d.events[event.ID] = event
	metadata, err := control.DecodeActive(event.Target)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := control.ActiveDocument(metadata)
	if err != nil || roundtrip != event.Target {
		t.Fatal("legacy metadata changed during round-trip", err, roundtrip)
	}
	replayed, created, err := r.EnqueuePublication(t.Context(), intent)
	if err != nil || created || replayed != event {
		t.Fatal("legacy publication cannot resume", err, replayed)
	}
	if _, err := r.AuthorizePublication(t.Context(), replayed); err != nil {
		t.Fatal("legacy publication cannot be delivered", err)
	}
}
func TestT12_07ControlFailureOrIncompleteCopiesCannotReachActiveMetadata(t *testing.T) {
	for _, kind := range []string{"commit", "one-copy", "not-ready", "wrong-generation", "already-published", "expected"} {
		t.Run(kind, func(t *testing.T) {
			r, d, i := publicationFixture(t)
			switch kind {
			case "commit":
				d.failCommit = true
			case "one-copy":
				d.copies = 1
			case "not-ready":
				d.ready = false
			case "wrong-generation":
				d.slot.Generation++
			case "already-published":
				d.unpublished = false
			case "expected":
				a := control.ActiveMetadata{Sequence: 1, EventID: "old", TaskID: "old-task", OwnerGeneration: 1, Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Standby, Slot: 0}, Generation: 1, BundleID: "C100"}}
				i.Expected = &a
			}
			calls := 0
			event, err := publication.Submit(t.Context(), r, deliveryFunc(func(context.Context, control.Publication) error { calls++; return nil }), i)
			if err == nil || event.ID != "" || calls != 0 || len(d.events) != 0 {
				t.Fatal(event, err, calls, d.events)
			}
		})
	}
}
func TestT12_16PendingEventBlocksDifferentIntentAndMismatchedExpectedRoute(t *testing.T) {
	r, d, i := publicationFixture(t)
	if _, _, err := r.EnqueuePublication(t.Context(), i); err != nil {
		t.Fatal(err)
	}
	i.ID = "publish-2"
	if _, _, err := r.EnqueuePublication(t.Context(), i); !errors.Is(err, control.ErrBusy) || len(d.events) != 1 {
		t.Fatal(err, d.events)
	}
}
func TestT12_16SameVersionShiftBindsConfirmedExpectedMetadata(t *testing.T) {
	r, d, i := publicationFixture(t)
	i.Kind = "route_shift"
	d.unpublished = false
	d.slot.AdmissionOpen = true
	old := control.ActiveMetadata{Sequence: 12, EventID: "old", TaskID: "old-task", OwnerGeneration: 3, Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 4, BundleID: "C101"}}
	i.Expected = &old
	doc, _ := control.ActiveDocument(old)
	d.confirmed = &doc
	e, _, err := r.EnqueuePublication(t.Context(), i)
	if err != nil || e.Expected == nil || *e.Expected != doc {
		t.Fatal(e, err)
	}
	if !strings.Contains(string(e.Target), `"generation":"8"`) {
		t.Fatal(e.Target)
	}
}

func TestT12_07UnconfirmedDeliveryPreservesCommittedPendingIntent(t *testing.T) {
	r, d, i := publicationFixture(t)
	lost := errors.New("delivery response lost")
	e, err := publication.Submit(t.Context(), r, deliveryFunc(func(context.Context, control.Publication) error { return lost }), i)
	if !errors.Is(err, lost) || e.ID != i.ID || d.events[i.ID].Status != "pending" || d.events[i.ID].ConfirmedAt != nil {
		t.Fatal(e, err, d.events)
	}
}
