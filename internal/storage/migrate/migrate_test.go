package migrate_test

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
)

type database struct {
	ledger       map[string]string
	connections  []string
	transactions []*transaction
	fail         string
}
type transaction struct {
	db                    *database
	pending               map[string]string
	statements            []string
	committed, rolledBack bool
}
type row struct{ value *string }

func (r row) Scan(dest ...any) error { *dest[0].(**string) = r.value; return nil }
func (d *database) Begin(_ context.Context, c storage.Connection) (storage.Transaction, error) {
	d.connections = append(d.connections, c.DSN())
	tx := &transaction{db: d, pending: map[string]string{}}
	d.transactions = append(d.transactions, tx)
	return tx, nil
}
func (tx *transaction) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	tx.statements = append(tx.statements, sql)
	if tx.db.fail != "" && strings.Contains(sql, tx.db.fail) {
		return 0, errors.New("injected execution failure")
	}
	if strings.HasPrefix(sql, "INSERT INTO migration_meta.versions") {
		tx.pending[args[0].(string)] = args[1].(string)
	}
	return 1, nil
}
func (tx *transaction) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	if !strings.Contains(sql, "WHERE version=$1") {
		panic("unbound ledger query")
	}
	if value, ok := tx.db.ledger[args[0].(string)]; ok {
		return row{value: &value}
	}
	return row{}
}
func (tx *transaction) Commit(context.Context) error {
	if tx.db.fail == "commit" {
		return errors.New("injected commit failure")
	}
	if tx.db.ledger == nil {
		tx.db.ledger = map[string]string{}
	}
	for k, v := range tx.pending {
		tx.db.ledger[k] = v
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

func registry(t *testing.T) storage.Registry {
	t.Helper()
	values := map[string]string{
		"DATABASE_M0_URL":      "postgres://m@db-m:5432/slot0?sslmode=disable",
		"DATABASE_M1_URL":      "postgres://m@db-m:5432/slot1?sslmode=disable",
		"DATABASE_S0_URL":      "postgres://s@db-s:5432/slot0?sslmode=disable",
		"DATABASE_S1_URL":      "postgres://s@db-s:5432/slot1?sslmode=disable",
		"DATABASE_CONTROL_URL": "postgres://c@db-m:5432/control?sslmode=disable",
		"DATABASE_LOG_URL":     "postgres://l@db-log:5432/logs?sslmode=disable",
	}
	r, err := storage.LoadRegistry(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestT02_06MigrationReplayConflictAndRollback(t *testing.T) {
	r := registry(t)
	target := migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: storage.Main}}
	plan, _ := migrate.Catalog(migrate.Business)
	db := &database{}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != 1 {
		t.Fatalf("initial migration: %d %v", n, err)
	}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != 0 {
		t.Fatalf("replay: %d %v", n, err)
	}
	for _, sql := range db.transactions[1].statements {
		if sql == plan[0].SQL {
			t.Fatal("replay executed business DDL")
		}
	}
	changed := []migrate.Migration{{Version: plan[0].Version, SQL: plan[0].SQL + "\n-- changed"}}
	if n, err := migrate.Apply(t.Context(), db, r, target, changed); err == nil || n != 0 {
		t.Fatal("checksum conflict accepted")
	}
	if db.ledger[plan[0].Version] != plan[0].Checksum() || !db.transactions[2].rolledBack {
		t.Fatal("conflict changed ledger or failed to roll back")
	}
	for _, failure := range []string{"CREATE SCHEMA source", "INSERT INTO migration_meta.versions", "commit"} {
		t.Run(failure, func(t *testing.T) {
			db := &database{fail: failure}
			if n, err := migrate.Apply(t.Context(), db, r, target, plan); err == nil || n != 0 {
				t.Fatal("failed migration reported applied")
			}
			if len(db.ledger) != 0 || !db.transactions[0].rolledBack {
				t.Fatal("partial migration persisted")
			}
		})
	}
}

func TestT11_12SQLExecutionLedgerMigrationIsAppendOnlyAndReplayable(t *testing.T) {
	plan, err := migrate.Catalog(migrate.Control)
	if err != nil || len(plan) < 2 || plan[1].Version != "control_002" {
		t.Fatal(plan, err)
	}
	plan = plan[:2]
	if !strings.Contains(plan[1].SQL, "REFERENCES control.readers(id)") || !strings.Contains(plan[1].SQL, "finished_at >= started_at") {
		t.Fatal("execution identity or time constraint missing")
	}
	db := &database{}
	target := migrate.Target{Domain: migrate.Control}
	r := registry(t)
	if n, err := migrate.Apply(t.Context(), db, r, target, plan[:1]); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestT11_16SnapshotMigrationUpgradesExecutionLedger(t *testing.T) {
	plan, err := migrate.Catalog(migrate.Control)
	if err != nil || len(plan) < 3 || plan[2].Version != "control_003" {
		t.Fatal(plan, err)
	}
	plan = plan[:3]
	db := &database{}
	target := migrate.Target{Domain: migrate.Control}
	r := registry(t)
	if n, err := migrate.Apply(t.Context(), db, r, target, plan[:2]); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestT14_14ReferenceMigrationUpgradesAndReplaysWithoutChangingHistory(t *testing.T) {
	plan, err := migrate.Catalog(migrate.Control)
	if err != nil || len(plan) < 5 || plan[4].Version != "control_005" {
		t.Fatal(plan, err)
	}
	db := &database{}
	target := migrate.Target{Domain: migrate.Control}
	r := registry(t)
	if n, err := migrate.Apply(t.Context(), db, r, target, plan[:4]); err != nil || n != 4 {
		t.Fatal(n, err)
	}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan[:5]); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan[:5]); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestT11_18BundleChecksMigrationUpgradesSnapshots(t *testing.T) {
	plan, err := migrate.Catalog(migrate.Control)
	if err != nil || len(plan) < 4 || plan[3].Version != "control_004" {
		t.Fatal(plan, err)
	}
	plan = plan[:4]
	db := &database{}
	target := migrate.Target{Domain: migrate.Control}
	r := registry(t)
	if n, err := migrate.Apply(t.Context(), db, r, target, plan[:3]); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestT02_13SourceDDLMatchesIndependentFieldPlan(t *testing.T) {
	plan, err := migrate.Catalog(migrate.Business)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/expected_columns.txt")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		p := strings.Split(line, "|")
		expected[p[0]+"."+p[1]] = p[2] + "|" + p[3]
	}
	counts := map[string]int{}
	pkCount := 0
	tables := regexp.MustCompile(`(?s)CREATE TABLE source\.(\w+) \((.*?)\n\);`).FindAllStringSubmatch(plan[0].SQL, -1)
	if len(tables) != 14 {
		t.Fatalf("source tables: %d", len(tables))
	}
	column := regexp.MustCompile(`^\s*"([A-Z_0-9]+)" (TIMESTAMP WITHOUT TIME ZONE|TEXT|JSON)(.*)$`)
	for _, table := range tables {
		for _, line := range strings.Split(strings.TrimSpace(table[2]), "\n") {
			m := column.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("unrecognized source column: %s", line)
			}
			pk := ""
			if strings.Contains(m[3], "PRIMARY KEY") {
				pk = "pk"
				pkCount++
				if m[2] != "TEXT" {
					t.Fatal("non-text primary key")
				}
			}
			key := table[1] + "." + m[1]
			if expected[key] != m[2]+"|"+pk {
				t.Fatalf("unexpected field/type/key %s", key)
			}
			delete(expected, key)
			counts[m[2]]++
		}
	}
	if len(expected) != 0 || pkCount != 14 || counts["TEXT"] != 256 || counts["JSON"] != 15 || counts["TIMESTAMP WITHOUT TIME ZONE"] != 3 {
		t.Fatalf("field plan mismatch: %v; remaining=%d; keys=%d", counts, len(expected), pkCount)
	}
	if strings.Contains(plan[0].SQL, "REFERENCES") || strings.Contains(plan[0].SQL, "JSONB") {
		t.Fatal("source DDL transforms storage or constrains missing relationships")
	}
	for _, statement := range strings.Split(plan[0].SQL, ";") {
		if strings.Contains(statement, "TO sicp_query") && strings.Contains(statement, "GRANT") {
			if strings.Contains(statement, "INSERT") || strings.Contains(statement, "UPDATE") || strings.Contains(statement, "DELETE") || strings.Contains(statement, "TRUNCATE") || strings.Contains(statement, "CREATE ON") {
				t.Fatal("query has write permissions")
			}
		}
	}
	if !strings.Contains(plan[0].SQL, "GRANT SELECT ON ALL TABLES IN SCHEMA source TO sicp_query") {
		t.Fatal("missing query read grant")
	}
}

func TestT02_13MigrationTargetsAndIsolation(t *testing.T) {
	r := registry(t)
	for _, tc := range []struct {
		target         migrate.Target
		suffix, schema string
	}{
		{migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: storage.Main}}, "db-m:5432/slot0?sslmode=disable", "source"},
		{migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: storage.Main, Slot: 1}}, "db-m:5432/slot1?sslmode=disable", "source"},
		{migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: storage.Standby}}, "db-s:5432/slot0?sslmode=disable", "source"},
		{migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: storage.Standby, Slot: 1}}, "db-s:5432/slot1?sslmode=disable", "source"},
		{migrate.Target{Domain: migrate.Control}, "db-m:5432/control?sslmode=disable", "control"},
		{migrate.Target{Domain: migrate.Logs}, "db-log:5432/logs?sslmode=disable", "logs"},
	} {
		db := &database{}
		plan, _ := migrate.Catalog(tc.target.Domain)
		if _, err := migrate.Apply(t.Context(), db, r, tc.target, plan); err != nil {
			t.Fatal(err)
		}
		if len(db.connections) != 1 || !strings.HasSuffix(db.connections[0], tc.suffix) {
			t.Fatal("wrong migration target")
		}
		if !strings.Contains(plan[0].SQL, "CREATE SCHEMA "+tc.schema+";") {
			t.Fatal("wrong schema")
		}
		if !strings.Contains(plan[0].SQL, "REVOKE ALL ON DATABASE %I FROM PUBLIC") || !strings.Contains(plan[0].SQL, "current_database()") || !strings.Contains(plan[0].SQL, "REVOKE CREATE ON SCHEMA public FROM PUBLIC") {
			t.Fatal("missing database isolation grants")
		}
		for _, schema := range []string{"source", "control", "logs"} {
			if schema != tc.schema && strings.Contains(plan[0].SQL, "CREATE TABLE "+schema+".") {
				t.Fatal("migration crossed domain")
			}
		}
		if db.transactions[0].statements[0] != "SELECT pg_advisory_xact_lock(734219001)" {
			t.Fatal("ledger was accessed before locking")
		}
	}
	for _, target := range []migrate.Target{{Domain: "unknown"}, {Domain: migrate.Business}, {Domain: migrate.Control, Position: storage.Position{Instance: storage.Main}}} {
		db := &database{}
		if _, err := migrate.Apply(t.Context(), db, r, target, nil); err == nil || len(db.connections) != 0 {
			t.Fatal("invalid target opened a connection")
		}
	}
	db := &database{}
	plan, _ := migrate.Catalog(migrate.Control)
	if _, err := migrate.Apply(t.Context(), db, r, migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: storage.Main}}, plan); err == nil || len(db.connections) != 0 {
		t.Fatal("control plan applied to business")
	}
}
