//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestI01_01(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	raw, err := os.ReadFile("../../internal/storage/migrate/testdata/expected_columns.txt")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		p := strings.Split(strings.TrimSpace(line), "|")
		expected[p[0]+"|"+p[1]] = p[2] + "|" + p[3]
	}
	for i := 0; i < 4; i++ {
		conn := e.connection(t, i)
		rows, err := conn.Query(e.ctx, `SELECT c.table_name,c.column_name,upper(c.data_type),EXISTS (
   SELECT 1 FROM information_schema.table_constraints tc JOIN information_schema.key_column_usage k
   USING (constraint_catalog,constraint_schema,constraint_name)
   WHERE tc.constraint_type='PRIMARY KEY' AND tc.table_schema=c.table_schema AND tc.table_name=c.table_name AND k.column_name=c.column_name)
   FROM information_schema.columns c WHERE c.table_schema='source'`)
		if err != nil {
			t.Fatal(err)
		}
		actual := map[string]string{}
		tables := map[string]bool{}
		for rows.Next() {
			var table, column, kind string
			var pk bool
			if err := rows.Scan(&table, &column, &kind, &pk); err != nil {
				t.Fatal(err)
			}
			suffix := ""
			if pk {
				suffix = "pk"
			}
			actual[table+"|"+column] = kind + "|" + suffix
			tables[table] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(tables) != 14 || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("slot %d schema mismatch: tables=%d columns=%d expected=%d", i, len(tables), len(actual), len(expected))
		}
	}
}

func TestI01_02(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	for _, target := range targets() {
		plan, err := migrate.Catalog(target.Domain)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan); err != nil || n != 0 {
			t.Fatalf("repeat: %d %v", n, err)
		}
		plan[0].SQL += "\n-- conflicting historical migration"
		if _, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan); err == nil || !strings.Contains(err.Error(), "checksum conflict") {
			t.Fatalf("conflict: %v", err)
		}
	}
}

// This gate is an external prerequisite fixture for the real storage writer.
// Publication/admission itself is exercised by the later integration groups.
type storageGate struct{}

func (storageGate) LockBuild(context.Context, source.Target) (source.Lease, error) {
	return storageLease{}, nil
}

type storageLease struct{}

func (storageLease) State() source.BuildState {
	return source.BuildState{Initializing: true, Generation: 1, CandidateID: "synthetic", OwnerValid: true}
}
func (storageLease) Release(context.Context) error { return nil }

type rowStream struct{ rows []source.SourceRow }

func (r *rowStream) Next(context.Context) (source.SourceRow, bool, error) {
	if len(r.rows) == 0 {
		return nil, false, nil
	}
	row := r.rows[0]
	r.rows = r.rows[1:]
	return row, true, nil
}
func (e *environment) rebuild(t *testing.T, table string, rows ...source.SourceRow) {
	t.Helper()
	r := source.Repository{Registry: e.registry, Database: e.db, Gate: storageGate{}}
	report, err := r.Rebuild(e.ctx, source.Target{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 1, CandidateID: "synthetic"}, table, &rowStream{rows: rows}, 1)
	if err != nil || !report.Complete || report.WrittenRows != len(rows) {
		t.Fatalf("rebuild: %+v %v", report, err)
	}
}

func TestI01_03(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	conn := e.connection(t, 0)
	// A unique role inherits only the production query grants, never worker grants.
	role := fmt.Sprintf("query_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), "DROP ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
			t.Error(err)
		}
	})
	if _, err := conn.Exec(e.ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+"; GRANT sicp_query TO "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(e.ctx, "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(e.ctx, "SELECT * FROM source.dwd_xqjbsjzlb"); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(e.ctx, `INSERT INTO source.dwd_xqjbsjzlb ("XQH") VALUES ('synthetic')`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("write was not denied: %v", err)
	}
	if err := tx.Rollback(e.ctx); err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{4, 5} {
		c := e.connection(t, index)
		if _, err := c.Exec(e.ctx, "CREATE TABLE public.integration_sentinel(value text); INSERT INTO public.integration_sentinel VALUES ('unchanged')"); err != nil {
			t.Fatal(err)
		}
	}
	e.rebuild(t, "dwd_xqjbsjzlb", source.SourceRow{"XQH": json.RawMessage(`"001"`)})
	e.rebuild(t, "dwd_xqjbsjzlb")
	for _, index := range []int{4, 5} {
		c := e.connection(t, index)
		var got string
		if err := c.QueryRow(e.ctx, "SELECT value FROM public.integration_sentinel").Scan(&got); err != nil || got != "unchanged" {
			t.Fatalf("fixed region changed: %q %v", got, err)
		}
	}
}

func TestI01_04(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	conn := e.connection(t, 0)
	// Explicit expected tokens are independent of the production encoder. JSON
	// (not JSONB) and TEXT are the exact column kinds used by the source catalog.
	if _, err := conn.Exec(e.ctx, "CREATE TABLE public.tokens (id int primary key, token json, code text)"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		raw, code string
		expected  *string
	}{
		{`9007199254740993`, `"000123"`, ptr("000123")},
		{`1234567890.12345678901234567890`, `"001.00"`, ptr("001.00")},
		{`1.2300e+20`, `null`, nil},
		{`null`, `""`, ptr("")},
	}
	connection, _ := e.registry.Business(storage.Position{Instance: storage.Main, Slot: 0})
	for i, c := range cases {
		token, err := value.Bind(value.JSON, value.Field{Present: true, Raw: json.RawMessage(c.raw)})
		if err != nil {
			t.Fatal(err)
		}
		code, err := value.Bind(value.Text, value.Field{Present: true, Raw: json.RawMessage(c.code)})
		if err != nil {
			t.Fatal(err)
		}
		tx, err := e.db.Begin(e.ctx, connection)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(e.ctx, "INSERT INTO public.tokens VALUES ($1,$2::json,$3)", i, token, code); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(e.ctx); err != nil {
				t.Fatal(err)
			}
		}()
		var actual string
		var actualCode *string
		if err := conn.QueryRow(e.ctx, "SELECT token::text,code FROM public.tokens WHERE id=$1", i).Scan(&actual, &actualCode); err != nil {
			t.Fatal(err)
		}
		if actual != c.raw || !reflect.DeepEqual(actualCode, c.expected) {
			t.Fatalf("token %d changed: %q %v", i, actual, actualCode)
		}
	}
}
func ptr(s string) *string { return &s }

func TestI01_05(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	e.rebuild(t, "dwd_xqjbsjzlb", source.SourceRow{"XQH": json.RawMessage(`"001"`), "ETL_TIME": json.RawMessage(`"2026-08-31T23:59:59.999999"`)})
	conn := e.connection(t, 0)
	if _, err := conn.Exec(e.ctx, "SET TIME ZONE 'America/New_York'"); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := conn.QueryRow(e.ctx, `SELECT to_char("ETL_TIME",'YYYY-MM-DD"T"HH24:MI:SS.US') FROM source.dwd_xqjbsjzlb WHERE "XQH"='001'`).Scan(&got); err != nil || got != "2026-08-31T23:59:59.999999" {
		t.Fatalf("timestamp changed: %q %v", got, err)
	}
	for _, raw := range []string{`"2026-08-31T23:59:59.9999999"`, `"2026-08-31T23:59:59+08:00"`} {
		if _, err := value.Bind(value.Timestamp, value.Field{Present: true, Raw: json.RawMessage(raw)}); err == nil {
			t.Fatalf("accepted lossy timestamp %s", raw)
		}
	}
}

func taskFixture(t *testing.T, e *environment) control.Repository {
	t.Helper()
	r := control.Repository{Registry: e.registry, Database: e.db}
	task := control.Task{ID: "task", IdempotencyKey: "key", Mode: "initialize", Status: "accepted", RequestedBy: "synthetic", AcceptedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Summary: "{}"}
	if err := r.Insert(e.ctx, &task); err != nil {
		t.Fatal(err)
	}
	return r
}
func TestI01_06(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := taskFixture(t, e)
	start := make(chan struct{})
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := r.Acquire(e.ctx, "task", fmt.Sprintf("worker-%d", i))
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, control.ErrBusy) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("owners=%d", success)
	}
	var matched bool
	if err := e.connection(t, 4).QueryRow(e.ctx, "SELECT e.owner_id=t.owner_id AND e.generation=t.owner_generation FROM control.execution e JOIN control.tasks t ON t.id=e.task_id").Scan(&matched); err != nil || !matched {
		t.Fatalf("ownership mismatch %v", err)
	}
}

type failCommitDB struct{ storage.Database }
type failCommitTx struct{ storage.Transaction }

var errCommit = errors.New("synthetic lost transaction before commit")

func (d failCommitDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.Database.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return failCommitTx{tx}, nil
}
func (failCommitTx) Commit(context.Context) error { return errCommit }
func TestI01_07(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := taskFixture(t, e)
	broken := control.Repository{Registry: e.registry, Database: failCommitDB{e.db}}
	if _, err := broken.Acquire(e.ctx, "task", "failed"); !errors.Is(err, errCommit) {
		t.Fatalf("fault: %v", err)
	}
	var free bool
	if err := e.connection(t, 4).QueryRow(e.ctx, "SELECT e.task_id IS NULL AND e.generation=0 AND t.owner_id IS NULL AND t.owner_generation=0 FROM control.execution e CROSS JOIN control.tasks t").Scan(&free); err != nil || !free {
		t.Fatalf("half ownership committed: %v", err)
	}
	first, err := r.Acquire(e.ctx, "task", "first")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Release(e.ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := r.Acquire(e.ctx, "task", "second")
	if err != nil {
		t.Fatal(err)
	}
	cp := control.Checkpoint{TaskID: "task", Name: "checkpoint", Payload: `{"synthetic":true}`, RecordedAt: time.Now().UTC()}
	if err := r.WithOwnership(first).Insert(e.ctx, &cp); !errors.Is(err, control.ErrOwnership) {
		t.Fatalf("stale mutation: %v", err)
	}
	if err := r.WithOwnership(second).Insert(e.ctx, &cp); err != nil {
		t.Fatal(err)
	}
	if err := r.Release(e.ctx, first); !errors.Is(err, control.ErrOwnership) {
		t.Fatalf("stale release: %v", err)
	}
}

// Exercise the bootstrap SQL against PostgreSQL, including slot/schedule facts
// that cannot be represented by the admission unit tests' task-only substitute.
func TestI01_08AutomaticBootstrapRequiresPristineControl(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := control.Repository{Registry: e.registry, Database: e.db}
	conn := e.connection(t, 4)
	req := control.Request{ID: "automatic", IdempotencyKey: "system-initialize-v1", Mode: "initialize", RequestedBy: "scheduler", AcceptedAt: time.Now().UTC()}
	for _, tc := range []struct{ name, setup, undo string }{
		{"bundle", "INSERT INTO control.bundles (id,tables,created_at) VALUES ('old','{}',now())", "DELETE FROM control.bundles WHERE id='old'"},
		{"slot_generation", "UPDATE control.slots SET generation=1 WHERE instance='M' AND slot=0", "UPDATE control.slots SET generation=0"},
		{"slot_state", "UPDATE control.slots SET state='building' WHERE instance='S' AND slot=1", "UPDATE control.slots SET state='empty'"},
		{"slot_readers", "UPDATE control.slots SET admission_open=TRUE WHERE instance='M' AND slot=1", "UPDATE control.slots SET admission_open=FALSE"},
		{"missing_slot", "DELETE FROM control.slots WHERE instance='S' AND slot=0", "INSERT INTO control.slots (instance,slot,state) VALUES ('S',0,'empty')"},
		{"schedule_revision", "UPDATE control.schedule SET revision=1", "UPDATE control.schedule SET revision=0"},
		{"schedule_start", "UPDATE control.schedule SET last_started_at=now()", "UPDATE control.schedule SET last_started_at=NULL"},
		{"schedule_due", "UPDATE control.schedule SET next_due_at=now()", "UPDATE control.schedule SET next_due_at=NULL"},
		{"schedule_deferred", "UPDATE control.schedule SET deferred=TRUE", "UPDATE control.schedule SET deferred=FALSE"},
		{"expired_cooldown", "UPDATE control.schedule SET cooldown_until=now()-interval '1 day'", "UPDATE control.schedule SET cooldown_until=NULL"},
		{"execution_generation", "UPDATE control.execution SET generation=1", "UPDATE control.execution SET generation=0"},
	} {
		if !t.Run(tc.name, func(t *testing.T) {
			if _, err := conn.Exec(e.ctx, tc.setup); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := conn.Exec(e.ctx, tc.undo); err != nil {
					t.Error(err)
				}
			})
			if result, err := r.Bootstrap(e.ctx, req); err != nil || result.Created {
				t.Fatalf("used environment admitted: %+v %v", result, err)
			}
			var count int
			if err := conn.QueryRow(e.ctx, "SELECT count(*) FROM control.tasks").Scan(&count); err != nil || count != 0 {
				t.Fatalf("unexpected bootstrap records: %d %v", count, err)
			}
		}) {
			t.FailNow()
		}
	}
	// Once every fixture is restored, independent worker requests must compete
	// on the real execution lock and produce exactly one accepted record.
	var group sync.WaitGroup
	for i := range 8 {
		group.Go(func() {
			request := req
			request.ID = fmt.Sprintf("automatic-%d", i)
			if _, err := r.Bootstrap(e.ctx, request); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	var id string
	var count int
	if err := conn.QueryRow(e.ctx, "SELECT min(id),count(*) FROM control.tasks").Scan(&id, &count); err != nil || count != 1 {
		t.Fatalf("concurrent bootstrap: count=%d err=%v", count, err)
	}
	if _, err := conn.Exec(e.ctx, "UPDATE control.tasks SET status='failed',finished_at=now()"); err != nil {
		t.Fatal(err)
	}
	if result, err := r.Bootstrap(e.ctx, req); err != nil || result.Created {
		t.Fatalf("failed initialization automatically retried: %+v %v", result, err)
	}
	rerun := control.Request{ID: "rerun", IdempotencyKey: "manual-rerun", RequestedBy: "admin", AcceptedAt: time.Now().UTC(), PredecessorID: &id}
	result, err := r.RerunTask(e.ctx, rerun)
	if err != nil || !result.Created || result.Task.Mode != "initialize" || result.Task.PredecessorID == nil || *result.Task.PredecessorID != id {
		t.Fatalf("failed initialization cannot be rerun: %+v %v", result, err)
	}
}

func TestI01_09RecentAutomaticTasksAreBoundedAndRedacted(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := control.Repository{Registry: e.registry, Database: e.db}
	empty, err := r.RecentTasks(e.ctx)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.Initialized || empty.NextDueAt != nil || empty.CooldownUntil != nil || empty.IntervalSeconds != 7200 {
		t.Fatalf("empty overview: %+v %v", empty, err)
	}
	conn := e.connection(t, 4)
	at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	if _, err := conn.Exec(e.ctx, `INSERT INTO control.tasks (id,idempotency_key,mode,status,requested_by,accepted_at,summary)
 SELECT 'task-' || lpad(n::text,2,'0'), 'private-key-' || n, 'incremental', 'failed', 'scheduler', $1, '{"private_evidence":"sensitive"}'::jsonb
 FROM generate_series(1,55) AS n`, at); err != nil {
		t.Fatal(err)
	}
	due, cooldown := at.Add(control.SyncInterval), at.Add(time.Minute)
	if _, err := conn.Exec(e.ctx, "UPDATE control.schedule SET next_due_at=$1,cooldown_until=$2", due, cooldown); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(e.ctx, `INSERT INTO control.publication_outbox (id,task_id,kind,target,status,created_at)
 VALUES ('publication','task-01','bundle_publish','{}','confirmed',$1)`, at); err != nil {
		t.Fatal(err)
	}
	out, err := r.RecentTasks(e.ctx)
	if err != nil || len(out.Items) != 50 || !out.Initialized || out.NextDueAt == nil || !out.NextDueAt.Equal(due) || out.CooldownUntil == nil || !out.CooldownUntil.Equal(cooldown) {
		t.Fatalf("overview: %+v %v", out, err)
	}
	for i, item := range out.Items {
		if item.ID != fmt.Sprintf("task-%02d", 55-i) || item.Operator != "scheduler" || !item.AcceptedAt.Equal(at) {
			t.Fatalf("unstable recent task order: %+v", item)
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-key", "private_evidence", "sensitive", "summary", "owner_id", "idempotency_key"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("overview exposed %s", private)
		}
	}
}
