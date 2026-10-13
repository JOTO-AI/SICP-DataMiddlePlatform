package migrate_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
)

// Each connection gets a distinct ledger; unlike the old single-target fixture,
// this exercises isolation when both migration domains run in one process.
type logDatabases struct {
	items                         map[storage.Connection]*database
	alias, changed, secretFailure bool
	transactions                  []*logTransaction
}
type logTransaction struct {
	*transaction
	owner      *logDatabases
	connection storage.Connection
	readOnly   bool
}
type scanRow func(...any) error

func (r scanRow) Scan(dest ...any) error { return r(dest...) }
func (d *logDatabases) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	if d.items == nil {
		d.items = map[storage.Connection]*database{}
	}
	item := d.items[c]
	if item == nil {
		item = &database{}
		d.items[c] = item
	}
	tx, err := item.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	result := &logTransaction{transaction: tx.(*transaction), owner: d, connection: c}
	d.transactions = append(d.transactions, result)
	return result, nil
}
func (t *logTransaction) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if sql == "SET TRANSACTION READ ONLY" {
		t.readOnly = true
		return 0, nil
	}
	if t.readOnly {
		return 0, errors.New("identity inspection attempted a write")
	}
	if t.owner.secretFailure {
		return 0, errors.New("postgres://user:synthetic-private-password@private-host")
	}
	return t.transaction.Exec(ctx, sql, args...)
}
func (t *logTransaction) QueryRow(ctx context.Context, sql string, args ...any) storage.Row {
	if strings.Contains(sql, "pg_catalog.pg_control_system()") {
		return scanRow(func(dest ...any) error {
			u, _ := url.Parse(t.connection.DSN())
			cluster := map[string]string{"db-m": "101", "db-s": "202", "db-log": "303", "db-upstream": "404"}[u.Hostname()]
			if u.Hostname() == "db-upstream" && t.owner.alias {
				cluster = "303"
			}
			if !t.readOnly && t.owner.changed {
				cluster = "505"
			}
			*dest[0].(*string) = cluster
			oid := int64(1)
			if u.Hostname() == "db-log" && u.Path == "/upstream_logs" {
				oid = 2
			}
			*dest[1].(*int64) = oid
			*dest[2].(*string) = strings.TrimPrefix(u.Path, "/")
			return nil
		})
	}
	if strings.Contains(sql, "left(version,length($1))") {
		return scanRow(func(dest ...any) error {
			foreign := false
			for version := range t.db.ledger {
				foreign = foreign || !strings.HasPrefix(version, args[0].(string))
			}
			*dest[0].(*bool) = foreign
			return nil
		})
	}
	return t.transaction.QueryRow(ctx, sql, args...)
}

func dualLogRegistry(t *testing.T) storage.Registry {
	return logRegistryAt(t, "db-upstream")
}

func logRegistryAt(t *testing.T, upstreamHost string) storage.Registry {
	t.Helper()
	v := map[string]string{
		"LOG_STORAGE_MODE":          "dual",
		"DATABASE_M0_URL":           "postgres://admin@db-m:5432/slot0?sslmode=disable",
		"DATABASE_M1_URL":           "postgres://admin@db-m:5432/slot1?sslmode=disable",
		"DATABASE_S0_URL":           "postgres://admin@db-s:5432/slot0?sslmode=disable",
		"DATABASE_S1_URL":           "postgres://admin@db-s:5432/slot1?sslmode=disable",
		"DATABASE_CONTROL_URL":      "postgres://admin@db-m:5432/control?sslmode=disable",
		"DATABASE_LOG_URL":          "postgres://admin@db-log:5432/logs?sslmode=disable",
		"DATABASE_UPSTREAM_LOG_URL": "postgres://admin@" + upstreamHost + ":5432/upstream_logs?sslmode=disable",
	}
	r, err := storage.LoadRegistry(func(k string) (string, bool) { value, ok := v[k]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestT28_04IndependentLogLedgersReplayAndConflict(t *testing.T) {
	for _, host := range []string{"db-upstream", "db-log"} {
		t.Run(host, func(t *testing.T) { checkLogLedgers(t, logRegistryAt(t, host)) })
	}
}

func checkLogLedgers(t *testing.T, r storage.Registry) {
	t.Helper()
	db := &logDatabases{}
	for _, domain := range []migrate.Domain{migrate.Logs, migrate.UpstreamLogs} {
		plan, err := migrate.Catalog(domain)
		if err != nil {
			t.Fatal(err)
		}
		target := migrate.Target{Domain: domain}
		if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != len(plan) {
			t.Fatal("first migration failed", n, err)
		}
		if n, err := migrate.Apply(t.Context(), db, r, target, plan); err != nil || n != 0 {
			t.Fatal("migration replay changed ledger", n, err)
		}
		changed := []migrate.Migration{{Version: plan[0].Version, SQL: plan[0].SQL + "\n-- conflicting migration"}}
		if n, err := migrate.Apply(t.Context(), db, r, target, changed); err == nil || n != 0 {
			t.Fatal("checksum conflict accepted")
		}
	}
	upstream, _ := r.LogConnection(storage.UpstreamLog)
	if len(db.items[r.Logs()].ledger) != 9 || len(db.items[upstream].ledger) != 8 {
		t.Fatal("logs share a ledger")
	}
	if _, ok := db.items[upstream].ledger["upstream_logs_001"]; !ok {
		t.Fatal("wrong upstream version")
	}
	for _, tx := range db.transactions {
		if !tx.committed && !tx.rolledBack {
			t.Fatal("migration or preflight transaction leaked")
		}
		if tx.readOnly && len(tx.statements) != 0 {
			t.Fatal("identity probe executed DDL")
		}
	}
}

func TestT28_04WrongTargetAliasAndChangedBackendRefuseDDL(t *testing.T) {
	r := dualLogRegistry(t)
	plan, _ := migrate.Catalog(migrate.UpstreamLogs)
	for _, target := range []migrate.Target{{Domain: migrate.Logs}, {Domain: migrate.Business, Position: storage.Position{Instance: storage.Main}}, {Domain: migrate.UpstreamLogs, Position: storage.Position{Instance: storage.Main}}} {
		db := &logDatabases{}
		if _, err := migrate.Apply(t.Context(), db, r, target, plan); err == nil || len(db.transactions) != 0 {
			t.Fatal("wrong domain reached a database")
		}
	}
	if _, err := migrate.Apply(t.Context(), &logDatabases{}, registry(t), migrate.Target{Domain: migrate.UpstreamLogs}, plan); err == nil {
		t.Fatal("legacy migration fell back to platform")
	}
	for _, db := range []*logDatabases{{alias: true}, {changed: true}} {
		if _, err := migrate.Apply(t.Context(), db, r, migrate.Target{Domain: migrate.UpstreamLogs}, plan); err != storage.ErrLogIsolation {
			t.Fatal("identity mismatch accepted", err)
		}
		for _, tx := range db.transactions {
			if len(tx.statements) != 0 {
				t.Fatal("DDL executed before isolation verification")
			}
		}
	}
	upstream, _ := r.LogConnection(storage.UpstreamLog)
	db := &logDatabases{items: map[storage.Connection]*database{upstream: {ledger: map[string]string{"logs_001": "foreign-checksum"}}}}
	if _, err := migrate.Apply(t.Context(), db, r, migrate.Target{Domain: migrate.UpstreamLogs}, plan); err == nil {
		t.Fatal("foreign ledger accepted")
	}
	if len(db.items[upstream].ledger) != 1 {
		t.Fatal("foreign ledger changed")
	}
}

func TestT28_04MigrationSchemaPermissionsAndErrorsAreBounded(t *testing.T) {
	plan, err := migrate.Catalog(migrate.UpstreamLogs)
	if err != nil || len(plan) != 8 || plan[0].Version != "upstream_logs_001" {
		t.Fatal("wrong upstream catalog", err)
	}
	for _, required := range []string{
		"REVOKE ALL ON DATABASE %I FROM sicp_log_maintenance",
		"REVOKE ALL ON SCHEMA logs FROM sicp_log_maintenance;",
		"TO sicp_upstream_maintenance;",
	} {
		if !strings.Contains(plan[4].SQL, required) {
			t.Fatal("shared-instance maintenance privilege boundary missing", required)
		}
	}
	for _, required := range []string{"CREATE SCHEMA logs;", "kind IN ('call','attempt')", "GRANT SELECT ON logs.events TO sicp_upstream_reader;", "GRANT SELECT, INSERT ON logs.events TO sicp_upstream_logger;"} {
		if !strings.Contains(plan[0].SQL, required) {
			t.Fatal("missing upstream schema boundary", required)
		}
	}
	for _, denied := range []string{"TO sicp_logger", "CREATE TABLE source.", "CREATE TABLE control.", "GRANT UPDATE", "GRANT DELETE", "GRANT TRUNCATE"} {
		if strings.Contains(plan[0].SQL, denied) {
			t.Fatal("upstream grants cross the boundary", denied)
		}
	}
	db := &logDatabases{secretFailure: true}
	if n, err := migrate.Apply(t.Context(), db, dualLogRegistry(t), migrate.Target{Domain: migrate.UpstreamLogs}, plan); n != 0 || err == nil || strings.Contains(err.Error(), "synthetic-private-password") || strings.Contains(err.Error(), "private-host") {
		t.Fatal("failed migration disclosed driver details", err)
	}
}
