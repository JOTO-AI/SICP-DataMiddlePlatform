package control_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func ptr[T any](v T) *T { return &v }

type dbCall struct {
	sql  string
	args []any
}
type db struct {
	calls       []dbCall
	connections []string
	raw         []any
	failCommit  bool
	failScan    bool
	writeCount  int64
	rollbacks   int
}
type tx struct {
	db        *db
	committed bool
}

func (d *db) Begin(_ context.Context, c storage.Connection) (storage.Transaction, error) {
	d.connections = append(d.connections, c.DSN())
	return &tx{db: d}, nil
}
func (t *tx) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	t.db.calls = append(t.db.calls, dbCall{sql, args})
	return t.db.writeCount, nil
}
func (t *tx) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	t.db.calls = append(t.db.calls, dbCall{sql, args})
	if strings.HasPrefix(sql, "SELECT task_id,owner_id,generation FROM control.execution") {
		return result{raw: []any{"task-A", "owner-A", int64(9)}}
	}
	if strings.HasPrefix(sql, "INSERT") {
		return result{raw: []any{int64(77)}}
	}
	return result{raw: t.db.raw, fail: t.db.failScan}
}
func (t *tx) Commit(context.Context) error {
	if t.db.failCommit {
		return errors.New("injected commit failure")
	}
	t.committed = true
	return nil
}
func (t *tx) Rollback(context.Context) error {
	if !t.committed {
		t.db.rollbacks++
	}
	return nil
}

type result struct {
	raw  []any
	fail bool
}

func (r result) Scan(dest ...any) error {
	if len(dest) != len(r.raw) {
		return errors.New("column count mismatch")
	}
	for i, input := range r.raw {
		v := reflect.ValueOf(dest[i]).Elem()
		if input == nil {
			v.SetZero()
			continue
		}
		if v.Kind() == reflect.Pointer {
			v.Set(reflect.New(v.Type().Elem()))
			v = v.Elem()
		}
		v.Set(reflect.ValueOf(input).Convert(v.Type()))
		if r.fail {
			return errors.New("injected scan failure")
		}
	}
	return nil
}
func repository(t *testing.T) (control.Repository, *db) {
	t.Helper()
	values := map[string]string{"DATABASE_M0_URL": "postgres://x@m:5432/b0?sslmode=disable", "DATABASE_M1_URL": "postgres://x@m:5432/b1?sslmode=disable", "DATABASE_S0_URL": "postgres://x@s:5432/b0?sslmode=disable", "DATABASE_S1_URL": "postgres://x@s:5432/b1?sslmode=disable", "DATABASE_CONTROL_URL": "postgres://control@m:5432/control?sslmode=disable", "DATABASE_LOG_URL": "postgres://x@l:5432/logs?sslmode=disable"}
	r, err := storage.LoadRegistry(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	d := &db{writeCount: 1}
	return control.Repository{Registry: r, Database: d}, d
}

func TestT02_14EveryEntityUsesFixedControlAndIndependentColumns(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	t1, t2, t3, t4, t5 := t0.Add(time.Minute), t0.Add(2*time.Minute), t0.Add(3*time.Minute), t0.Add(4*time.Minute), t0.Add(5*time.Minute)
	for _, tc := range []struct {
		name, columns       string
		entity, destination any
		raw                 []any
		keys                []any
	}{
		{"tasks", "id,idempotency_key,mode,status,requested_by,predecessor_id,accepted_at,started_at,ready_at,published_at,checked_at,finished_at,owner_id,owner_generation,roles,summary",
			&control.Task{ID: "task-A", IdempotencyKey: "idem-A", Mode: "incremental", Status: "published", RequestedBy: "admin-A", PredecessorID: ptr("task-old"), AcceptedAt: t0, StartedAt: &t1, ReadyAt: &t2, PublishedAt: &t3, CheckedAt: &t4, OwnerID: ptr("owner-A"), OwnerGeneration: 9, Roles: ptr(control.Document(`{"first":"S"}`)), Summary: "{}"},
			&control.Task{ID: "task-A"}, []any{"task-A", "idem-A", "incremental", "published", "admin-A", "task-old", t0, t1, t2, t3, t4, nil, "owner-A", int64(9), `{"first":"S"}`, "{}"}, []any{"task-A"}},
		{"steps", "task_id,name,status,started_at,finished_at,evidence", &control.Step{TaskID: "task-A", Name: "fetch", Status: "running", StartedAt: &t1, Evidence: ptr(control.Document("{}"))}, &control.Step{TaskID: "task-A", Name: "fetch"}, []any{"task-A", "fetch", "running", t1, nil, "{}"}, []any{"task-A", "fetch"}},
		{"checkpoints", "task_id,name,payload,recorded_at", &control.Checkpoint{TaskID: "task-A", Name: "sealed", Payload: `{"pages":2}`, RecordedAt: t2}, &control.Checkpoint{TaskID: "task-A", Name: "sealed"}, []any{"task-A", "sealed", `{"pages":2}`, t2}, []any{"task-A", "sealed"}},
		{"schedule", "singleton,revision,last_started_at,next_due_at,deferred,cooldown_until", &control.Schedule{Singleton: true, Revision: 3, LastStartedAt: &t1, NextDueAt: &t4, Deferred: true, CooldownUntil: &t5}, &control.Schedule{Singleton: true}, []any{true, int64(3), t1, t4, true, t5}, []any{true}},
		{"bundles", "id,tables,created_at,published_at,checked_at", &control.Bundle{ID: "C100", Tables: `{"table":"V1"}`, CreatedAt: t0, PublishedAt: &t3, CheckedAt: &t4}, &control.Bundle{ID: "C100"}, []any{"C100", `{"table":"V1"}`, t0, t3, t4}, []any{"C100"}},
		{"slots", "instance,slot,generation,bundle_id,admission_open,state", &control.Slot{Instance: "S", Slot: 1, Generation: 7, BundleID: ptr("C100"), State: "ready"}, &control.Slot{Instance: "S", Slot: 1}, []any{"S", int16(1), int64(7), "C100", false, "ready"}, []any{"S", int16(1)}},
		{"candidates", "id,task_id,instance,slot,generation,bundle_id,owner_generation,state,evidence,ready_at", &control.Candidate{ID: "candidate-A", TaskID: "task-A", Instance: "M", Generation: 8, BundleID: "C101", OwnerGeneration: 9, State: "building", Evidence: ptr(control.Document("{}"))}, &control.Candidate{ID: "candidate-A"}, []any{"candidate-A", "task-A", "M", int16(0), int64(8), "C101", int64(9), "building", "{}", nil}, []any{"candidate-A"}},
		{"source_progress", "source,confirmed_until,checked_at,task_id", &control.Progress{Source: "dwd_xqjbsjzlb", ConfirmedUntil: t1, CheckedAt: t4, TaskID: "task-A"}, &control.Progress{Source: "dwd_xqjbsjzlb"}, []any{"dwd_xqjbsjzlb", t1, t4, "task-A"}, []any{"dwd_xqjbsjzlb"}},
		{"publication_outbox", "id,sequence,task_id,kind,expected,target,status,created_at,applied_at,confirmed_at", &control.Publication{ID: "pub-A", TaskID: "task-A", Kind: "route_shift", Expected: ptr(control.Document(`{"instance":"M"}`)), Target: `{"instance":"S"}`, Status: "pending", CreatedAt: t2}, &control.Publication{ID: "pub-A"}, []any{"pub-A", int64(77), "task-A", "route_shift", `{"instance":"M"}`, `{"instance":"S"}`, "pending", t2, nil, nil}, []any{"pub-A"}},
		{"notification_outbox", "id,failure_key,task_id,payload,status,attempts,created_at,next_attempt_at,sent_at", &control.Notification{ID: "notice-A", FailureKey: "failure-A", TaskID: "task-A", Payload: "{}", Status: "pending", Attempts: 2, CreatedAt: t0, NextAttemptAt: &t5}, &control.Notification{ID: "notice-A"}, []any{"notice-A", "failure-A", "task-A", "{}", "pending", 2, t0, t5, nil}, []any{"notice-A"}},
		{"readers", "id,instance,slot,generation,bundle_id,process_id,admitted_at,heartbeat_at,sql_finished_at,released_at", &control.Reader{ID: "reader-A", Instance: "S", Slot: 1, Generation: 7, BundleID: "C100", ProcessID: "process-A", AdmittedAt: t1, HeartbeatAt: &t2}, &control.Reader{ID: "reader-A"}, []any{"reader-A", "S", int16(1), int64(7), "C100", "process-A", t1, t2, nil, nil}, []any{"reader-A"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, d := repository(t)
			d.raw = tc.raw
			if tc.name == "candidates" || tc.name == "source_progress" {
				r = r.WithOwnership(control.Ownership{TaskID: "task-A", OwnerID: "owner-A", Generation: 9})
			}
			if err := r.Insert(t.Context(), tc.entity); err != nil {
				t.Fatal(err)
			}
			var writes []dbCall
			for _, call := range d.calls {
				if !strings.Contains(call.sql, "FROM control.execution") {
					writes = append(writes, call)
				}
			}
			d.calls = writes
			var writeColumns []string
			var writeValues []any
			for i, name := range strings.Split(tc.columns, ",") {
				if name == "sequence" {
					continue
				}
				writeColumns = append(writeColumns, `"`+name+`"`)
				writeValues = append(writeValues, tc.raw[i])
			}
			if !strings.HasPrefix(d.calls[0].sql, "INSERT INTO control."+tc.name+" ("+strings.Join(writeColumns, ",")+") VALUES (") || !reflect.DeepEqual(d.calls[0].args, writeValues) {
				t.Fatalf("write mapping mismatch: %s %#v", d.calls[0].sql, d.calls[0].args)
			}
			_, _ = r.Registry.Business(storage.Position{Instance: storage.Standby, Slot: 1})
			if err := r.Load(t.Context(), tc.destination); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(tc.destination, tc.entity) {
				t.Fatalf("read changed fields/times: %#v != %#v", tc.destination, tc.entity)
			}
			if !reflect.DeepEqual(d.calls[1].args, tc.keys) || !strings.Contains(d.calls[1].sql, " FROM control."+tc.name+" WHERE ") {
				t.Fatal("wrong read identity or domain")
			}
			for _, connection := range d.connections {
				if connection != "postgres://control@m:5432/control?sslmode=disable" {
					t.Fatal("control access followed business route")
				}
			}
		})
	}
}

func TestT02_14UpdatesAndFailuresDoNotFabricateFacts(t *testing.T) {
	r, d := repository(t)
	s := &control.Schedule{Singleton: true, Revision: 4}
	if err := r.Update(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if d.calls[0].sql != `UPDATE control.schedule SET "revision"=$1,"last_started_at"=$2,"next_due_at"=$3,"deferred"=$4,"cooldown_until"=$5 WHERE "singleton"=$6` || !reflect.DeepEqual(d.calls[0].args, []any{int64(4), nil, nil, false, nil, true}) {
		t.Fatal("update misplaced key/time fields")
	}
	d.writeCount = 0
	if err := r.Update(t.Context(), s); !errors.Is(err, control.ErrMissing) {
		t.Fatal("absent update reported success")
	}
	for _, scanFailure := range []bool{true, false} {
		r, d := repository(t)
		d.raw = []any{true, int64(7), nil, nil, true, nil}
		d.failScan = scanFailure
		d.failCommit = !scanFailure
		original := control.Schedule{Singleton: true, Revision: 42}
		destination := original
		if err := r.Load(t.Context(), &destination); err == nil || !reflect.DeepEqual(destination, original) || d.rollbacks != 1 {
			t.Fatal("failed load partially changed destination")
		}
	}
	r, d = repository(t)
	d.failCommit = true
	p := &control.Publication{ID: "event-A", TaskID: "task-A", Kind: "bundle_publish", Target: "{}", Status: "pending", CreatedAt: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)}
	if err := r.Insert(t.Context(), p); err == nil || p.Sequence != 0 {
		t.Fatal("failed commit exposed generated sequence")
	}
	for _, entity := range []any{nil, (*control.Task)(nil), &struct{ ID string }{ID: "bad"}, &control.Bundle{ID: "bundle", Tables: "invalid"}, &control.Task{Summary: "{}"}} {
		r, d := repository(t)
		if err := r.Insert(t.Context(), entity); err == nil || len(d.connections) != 0 {
			t.Fatal("invalid entity opened database")
		}
	}
}
