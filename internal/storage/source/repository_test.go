package source_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type iterator struct {
	rows   []source.SourceRow
	index  int
	failAt int
}

func (i *iterator) Next(context.Context) (source.SourceRow, bool, error) {
	if i.failAt > 0 && i.index == i.failAt {
		return nil, false, errors.New("injected input failure")
	}
	if i.index == len(i.rows) {
		return nil, false, nil
	}
	r := i.rows[i.index]
	i.index++
	return r, true, nil
}

type gate struct {
	state      source.BuildState
	released   int
	releaseErr error
}

func (g *gate) LockBuild(context.Context, source.Target) (source.Lease, error) { return g, nil }
func (g *gate) State() source.BuildState                                       { return g.state }
func (g *gate) Release(context.Context) error                                  { g.released++; return g.releaseErr }

type call struct {
	sql  string
	args []any
}
type database struct {
	transactions []*transaction
	calls        []call
	failBatch    int
	failCommit   bool
	readRows     [][]any
	rowError     error
	closed       bool
}
type transaction struct {
	db                    *database
	number                int
	committed, rolledBack bool
}

func (d *database) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	tx := &transaction{db: d, number: len(d.transactions) + 1}
	d.transactions = append(d.transactions, tx)
	return tx, nil
}
func (tx *transaction) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	tx.db.calls = append(tx.db.calls, call{sql, append([]any(nil), args...)})
	if tx.number == tx.db.failBatch {
		return 0, errors.New("injected write failure")
	}
	if strings.HasPrefix(sql, "INSERT INTO") {
		return int64(len(args) / 4), nil
	}
	return 0, nil
}
func (tx *transaction) QueryRow(_ context.Context, sql string, _ ...any) storage.Row {
	if strings.Contains(sql, "has_table_privilege") {
		return indexResult(true)
	}
	panic("unexpected QueryRow")
}
func (tx *transaction) Query(_ context.Context, sql string, args ...any) (storage.Rows, error) {
	tx.db.calls = append(tx.db.calls, call{sql, args})
	return &rows{db: tx.db}, nil
}
func (tx *transaction) Commit(context.Context) error {
	if tx.db.failCommit {
		return errors.New("injected commit failure")
	}
	tx.committed = true
	return nil
}
func (tx *transaction) Rollback(context.Context) error {
	if !tx.committed {
		tx.rolledBack = true
	}
	return nil
}

type rows struct {
	db    *database
	index int
}

func (r *rows) Next() bool {
	if r.index == len(r.db.readRows) {
		return false
	}
	r.index++
	return true
}
func (r *rows) Scan(dest ...any) error {
	for i, v := range r.db.readRows[r.index-1] {
		*dest[i].(*any) = v
	}
	return nil
}
func (r *rows) Err() error { return r.db.rowError }
func (r *rows) Close()     { r.db.closed = true }

func setup(t *testing.T) (source.Repository, *database, *gate, source.Target) {
	t.Helper()
	values := map[string]string{"DATABASE_M0_URL": "postgres://x@m:5432/b0?sslmode=disable", "DATABASE_M1_URL": "postgres://x@m:5432/b1?sslmode=disable", "DATABASE_S0_URL": "postgres://x@s:5432/b0?sslmode=disable", "DATABASE_S1_URL": "postgres://x@s:5432/b1?sslmode=disable", "DATABASE_CONTROL_URL": "postgres://x@m:5432/control?sslmode=disable", "DATABASE_LOG_URL": "postgres://x@l:5432/logs?sslmode=disable"}
	values["LOG_STORAGE_MODE"] = "single_legacy"
	r, err := storage.LoadRegistry(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	db := &database{}
	g := &gate{state: source.BuildState{ActiveInstance: storage.Main, Generation: 2, CandidateID: "candidate-2", OwnerValid: true}}
	target := source.Target{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 2, CandidateID: "candidate-2"}
	return source.Repository{Registry: r, Database: db, Gate: g}, db, g, target
}
func campus(id string) source.SourceRow {
	return source.SourceRow{"XQH": json.RawMessage(`"` + id + `"`), "XQMC": json.RawMessage(`"campus"`)}
}

func TestBT03_StatisticsFailureCannotCompleteTable(t *testing.T) {
	r, db, g, target := setup(t)
	db.failBatch = 2 // First write commits; post-import ANALYZE transaction fails.
	report, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", &iterator{rows: []source.SourceRow{campus("001")}}, 2)
	if err == nil || report.Complete || report.WrittenRows != 1 || g.released != 1 {
		t.Fatal(report, err)
	}
	if !db.transactions[1].rolledBack {
		t.Fatal("statistics failure not rolled back")
	}
}

func TestT02_07SecondBatchFailureReportsOnlyCommittedBatches(t *testing.T) {
	r, db, g, target := setup(t)
	db.failBatch = 2
	input := &iterator{rows: []source.SourceRow{campus("001"), campus("002"), campus("003")}}
	report, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", input, 1)
	if err == nil || report.Complete || report.CompletedBatches != 1 || report.WrittenRows != 1 {
		t.Fatalf("partial write reported incorrectly: %+v %v", report, err)
	}
	if len(db.transactions) != 2 || !db.transactions[0].committed || !db.transactions[1].rolledBack || g.released != 1 {
		t.Fatal("transaction/lease lifecycle incorrect")
	}
	if db.calls[0].sql != "TRUNCATE TABLE source.dwd_xqjbsjzlb" || !reflect.DeepEqual(db.calls[1].args, []any{"001", "campus", nil, nil}) {
		t.Fatal("incorrect target or full-row bindings")
	}
	if strings.Contains(db.calls[1].sql, "001") || strings.Contains(db.calls[1].sql, "ON CONFLICT") {
		t.Fatal("input interpolated or duplicate keys silently merged")
	}
}

func TestT02_07CompletedEmptyInputAndFailurePaths(t *testing.T) {
	for _, n := range []int{0, 1, 3} {
		r, db, g, target := setup(t)
		input := &iterator{}
		for i := 0; i < n; i++ {
			input.rows = append(input.rows, campus(string(rune('a'+i))))
		}
		report, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", input, 2)
		expectedBatches := (n + 1) / 2
		if expectedBatches == 0 {
			expectedBatches = 1
		}
		if err != nil || !report.Complete || report.WrittenRows != n || report.CompletedBatches != expectedBatches || g.released != 1 {
			t.Fatalf("completion: %+v %v", report, err)
		}
		truncates := 0
		for _, c := range db.calls {
			if strings.HasPrefix(c.sql, "TRUNCATE") {
				truncates++
			}
		}
		if truncates != 1 {
			t.Fatal("must truncate exactly once")
		}
	}
	r, db, _, target := setup(t)
	db.failCommit = true
	if report, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", &iterator{rows: []source.SourceRow{campus("001")}}, 1); err == nil || report.Complete || report.CompletedBatches != 0 {
		t.Fatal("commit error reported success")
	}
	r, _, g, target := setup(t)
	g.releaseErr = errors.New("injected release failure")
	if report, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", &iterator{}, 1); err == nil || report.Complete {
		t.Fatal("lease release failure ignored")
	}
	r, db, _, target = setup(t)
	if report, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", &iterator{rows: []source.SourceRow{campus("001"), campus("002")}, failAt: 1}, 1); err == nil || report.Complete || report.CompletedBatches != 1 || len(db.transactions) != 1 {
		t.Fatal("input failure lost prior batch status")
	}
}

func TestT02_08UnsafeTargetsNeverOpenWriteTransactions(t *testing.T) {
	for _, change := range []func(*source.Target, *gate){
		func(x *source.Target, g *gate) { x.Position.Instance = storage.Main },
		func(x *source.Target, g *gate) { x.Position.Instance = "control" },
		func(x *source.Target, g *gate) { x.Position.Instance = "logs" },
		func(x *source.Target, g *gate) { x.Position.Slot = 2 },
		func(x *source.Target, g *gate) { x.Generation = 1 },
		func(x *source.Target, g *gate) { g.state.Readers = 1 },
		func(x *source.Target, g *gate) { g.state.Readers = -1 },
		func(x *source.Target, g *gate) { g.state.AdmissionOpen = true },
		func(x *source.Target, g *gate) { g.state.OwnerValid = false },
		func(x *source.Target, g *gate) { g.state.CandidateID = "other" },
		func(x *source.Target, g *gate) { g.state.ActiveInstance = "unknown" },
		func(x *source.Target, g *gate) { g.state.ActiveInstance = "" },
		func(x *source.Target, g *gate) { g.state.Initializing = true },
	} {
		r, db, g, target := setup(t)
		change(&target, g)
		if _, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", &iterator{}, 1); err == nil || len(db.transactions) != 0 || len(db.calls) != 0 {
			t.Fatal("unsafe target executed SQL")
		}
	}
	for _, table := range []string{"control.tasks", "logs.events", "dwd_xqjbsjzlb; DROP SCHEMA control CASCADE"} {
		r, db, _, target := setup(t)
		if _, err := r.Rebuild(t.Context(), target, table, &iterator{}, 1); err == nil || len(db.calls) != 0 {
			t.Fatal("invalid table executed SQL")
		}
	}
	r, db, _, target := setup(t)
	bad := campus("001")
	bad["UNKNOWN"] = json.RawMessage("null")
	if _, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", &iterator{rows: []source.SourceRow{bad}}, 1); err == nil || len(db.calls) != 0 {
		t.Fatal("unknown source column was discarded")
	}
	r, db, g, target := setup(t)
	g.state.ActiveInstance = ""
	g.state.Initializing = true
	if report, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", &iterator{}, 1); err != nil || !report.Complete {
		t.Fatal("explicit initialization refused", err)
	}
	r, db, _, target = setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if report, err := r.Rebuild(ctx, target, "dwd_xqjbsjzlb", &iterator{}, 1); err == nil || report.Complete || len(db.calls) != 0 {
		t.Fatal("canceled rebuild performed writes")
	}
}

func TestT02_07ReadBatchBindsCursorAndFailsWholeResult(t *testing.T) {
	r, db, _, target := setup(t)
	db.readRows = [][]any{{"0001", "campus", nil, time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)}}
	result, err := r.ReadBatch(t.Context(), target.Position, "dwd_xqjbsjzlb", "x' OR TRUE --", 2)
	if err != nil || len(result) != 1 || string(result[0]["XQH"]) != `"0001"` || result[0]["XQCX"] != nil || string(result[0]["ETL_TIME"]) != `"2024-01-02T03:04:05"` {
		t.Fatalf("read result: %+v %v", result, err)
	}
	if strings.Contains(db.calls[0].sql, "OR TRUE") || !reflect.DeepEqual(db.calls[0].args, []any{"x' OR TRUE --", 2}) || !strings.Contains(db.calls[0].sql, `COLLATE "C" LIMIT $2`) || !db.closed {
		t.Fatal("unbounded/unbound query or rows not closed")
	}
	db.rowError = errors.New("injected later read failure")
	if result, err := r.ReadBatch(t.Context(), target.Position, "dwd_xqjbsjzlb", "", 2); err == nil || result != nil {
		t.Fatal("partial read returned success")
	}
	db.rowError = nil
	db.readRows = append(db.readRows, db.readRows[0])
	if result, err := r.ReadBatch(t.Context(), target.Position, "dwd_xqjbsjzlb", "", 1); err == nil || result != nil {
		t.Fatal("limit violation returned partial success")
	}
}

func TestT02_08CatalogMatchesIndependentFieldPlan(t *testing.T) {
	data, err := os.ReadFile("../migrate/testdata/expected_columns.txt")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		p := strings.Split(line, "|")
		expected[p[0]+"."+p[1]] = p[2] + "|" + p[3]
	}
	catalog := source.Catalog()
	if len(catalog) != 14 {
		t.Fatal("missing source tables")
	}
	for _, table := range catalog {
		for _, column := range table.Columns {
			pk := ""
			if column.Name == table.PrimaryKey {
				pk = "pk"
			}
			key := table.Name + "." + column.Name
			if expected[key] != string(column.Kind)+"|"+pk {
				t.Fatalf("column mismatch: %s", key)
			}
			delete(expected, key)
		}
	}
	if len(expected) != 0 {
		t.Fatal("missing source columns")
	}
	catalog[0].Columns[0].Name = "MUTATED"
	if source.Catalog()[0].Columns[0].Name == "MUTATED" {
		t.Fatal("caller mutated identifier allowlist")
	}
}

func TestT06_08NamesAreBoundAsLiteralSubstrings(t *testing.T) {
	var previous string
	for _, name := range []string{"campus", `x' OR TRUE --`, `100%_\\`, "中文"} {
		r, db, _, target := setup(t)
		_, err := r.ReadMatching(t.Context(), target.Position, "dwd_xqjbsjzlb", "", 10, []source.Filter{{Column: "XQMC", Value: name, Contains: true}, {Column: "XQH", Value: "001"}})
		if err != nil {
			t.Fatal(err)
		}
		call := db.calls[0]
		if !reflect.DeepEqual(call.args, []any{"", 10, name, "001"}) || !strings.Contains(call.sql, `strpos("XQMC", $3) > 0`) || !strings.Contains(call.sql, `"XQH" COLLATE "C" = $4`) || !db.closed {
			t.Fatal(call)
		}
		if previous != "" && previous != call.sql {
			t.Fatal("input changed SQL structure")
		}
		previous = call.sql
	}
	for _, column := range []string{`XQH" OR TRUE --`, "unknown", "ETL_TIME"} {
		r, db, _, target := setup(t)
		if _, err := r.ReadMatching(t.Context(), target.Position, "dwd_xqjbsjzlb", "", 10, []source.Filter{{Column: column, Value: "x"}}); err == nil || len(db.calls) != 0 {
			t.Fatal(column, err, db.calls)
		}
	}
}

func TestT06_12MatchingReadsDiscardLaterFailure(t *testing.T) {
	r, db, _, target := setup(t)
	db.readRows = [][]any{{"0001", "campus", nil, nil}}
	db.rowError = errors.New("later cursor failure")
	if rows, err := r.ReadMatching(t.Context(), target.Position, "dwd_xqjbsjzlb", "", 10, []source.Filter{{Column: "XQMC", Value: "campus", Contains: true}}); err == nil || rows != nil || !db.closed {
		t.Fatal(rows, err)
	}
}
