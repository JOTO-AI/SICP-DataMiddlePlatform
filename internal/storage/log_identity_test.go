package storage_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type identityFact struct {
	cluster string
	oid     int64
	name    string
}
type identityDB struct {
	facts        map[storage.Connection]identityFact
	fail         string
	transactions []*identityTx
}
type identityTx struct {
	db               *identityDB
	c                storage.Connection
	readOnly, closed bool
}
type identityRow struct {
	fact identityFact
	err  error
}

func (r identityRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = r.fact.cluster
	*dest[1].(*int64) = r.fact.oid
	*dest[2].(*string) = r.fact.name
	return nil
}
func (d *identityDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	if d.fail == "begin" || ctx.Err() != nil {
		return nil, errors.New("secret-dsn")
	}
	tx := &identityTx{db: d, c: c}
	d.transactions = append(d.transactions, tx)
	return tx, nil
}
func (t *identityTx) Exec(_ context.Context, sql string, _ ...any) (int64, error) {
	if t.db.fail == "readonly" || sql != "SET TRANSACTION READ ONLY" {
		return 0, errors.New("secret-dsn")
	}
	t.readOnly = true
	return 0, nil
}
func (t *identityTx) QueryRow(_ context.Context, sql string, _ ...any) storage.Row {
	if t.db.fail == "scan" || !strings.Contains(sql, "pg_catalog.pg_control_system()") || !strings.Contains(sql, "pg_catalog.pg_database") {
		return identityRow{err: errors.New("secret-dsn")}
	}
	return identityRow{fact: t.db.facts[t.c]}
}
func (t *identityTx) Commit(context.Context) error {
	return errors.New("identity probe must not commit")
}
func (t *identityTx) Rollback(ctx context.Context) error {
	t.closed = true
	if t.db.fail == "rollback" || ctx.Err() != nil {
		return errors.New("secret-dsn")
	}
	return nil
}

func identityFixture(t *testing.T) (storage.Registry, *identityDB) {
	t.Helper()
	r, err := load(dualValues())
	if err != nil {
		t.Fatal(err)
	}
	db := &identityDB{facts: map[storage.Connection]identityFact{}}
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		for slot := 0; slot < 2; slot++ {
			c, err := r.Business(storage.Position{Instance: instance, Slot: slot})
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(c.DSN())
			cluster := "101"
			if instance == storage.Standby {
				cluster = "202"
			}
			db.facts[c] = identityFact{cluster, int64(slot + 1), strings.TrimPrefix(u.Path, "/")}
		}
	}
	db.facts[r.Control()] = identityFact{"101", 3, "sicp_control"}
	db.facts[r.Logs()] = identityFact{"303", 1, "sicp_log"}
	c, err := r.LogConnection(storage.UpstreamLog)
	if err != nil {
		t.Fatal(err)
	}
	db.facts[c] = identityFact{"404", 1, "upstream_logs"}
	return r, db
}

func TestT28_01PhysicalIdentityRejectsAliasesAndRechecksWriteTarget(t *testing.T) {
	r, db := identityFixture(t)
	proof, err := storage.VerifyLogIsolation(t.Context(), db, r)
	if err != nil || len(db.transactions) != 7 {
		t.Fatal("valid isolation rejected", err)
	}
	for _, tx := range db.transactions {
		if !tx.readOnly || !tx.closed {
			t.Fatal("unbounded or writeable identity probe")
		}
	}
	upstream, _ := r.LogConnection(storage.UpstreamLog)
	tx, _ := db.Begin(t.Context(), upstream)
	defer tx.Rollback(context.Background())
	if err := proof.Match(t.Context(), tx, upstream); err != nil {
		t.Fatal(err)
	}
	fact := db.facts[upstream]
	fact.cluster = "505"
	db.facts[upstream] = fact
	if err := proof.Match(t.Context(), tx, upstream); !errors.Is(err, storage.ErrLogIsolation) {
		t.Fatal("backend changed after probe", err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", proof, proof), "secret") {
		t.Fatal("identity proof leaks configuration")
	}
	for _, cluster := range []string{"101", "202", "303"} {
		r, db := identityFixture(t)
		upstream, _ := r.LogConnection(storage.UpstreamLog)
		fact := db.facts[upstream]
		fact.cluster = cluster
		db.facts[upstream] = fact
		if _, err := storage.VerifyLogIsolation(t.Context(), db, r); !errors.Is(err, storage.ErrLogIsolation) {
			t.Fatal("different hostname masked shared physical cluster", err)
		}
	}
	r, db = identityFixture(t)
	fact = db.facts[r.Logs()]
	fact.cluster = "101"
	db.facts[r.Logs()] = fact
	if _, err := storage.VerifyLogIsolation(t.Context(), db, r); !errors.Is(err, storage.ErrLogIsolation) {
		t.Fatal("platform aliases business cluster", err)
	}
}

func TestT28_01UnavailableOrMalformedIdentityNeverVerifies(t *testing.T) {
	for _, failure := range []string{"begin", "readonly", "scan", "rollback"} {
		r, db := identityFixture(t)
		db.fail = failure
		proof, err := storage.VerifyLogIsolation(t.Context(), db, r)
		if err != storage.ErrLogIdentity || strings.Contains(err.Error(), "secret-dsn") {
			t.Fatal("identity failure misreported", err)
		}
		if err := proof.Match(t.Context(), nil, r.Logs()); err != storage.ErrLogIdentity {
			t.Fatal("partial proof escaped")
		}
		for _, tx := range db.transactions {
			if !tx.closed {
				t.Fatal("probe transaction leaked")
			}
		}
	}
	for _, fact := range []identityFact{{}, {"0", 1, "sicp_log"}, {"0303", 1, "sicp_log"}, {"303", 0, "sicp_log"}, {"303", 1 << 32, "sicp_log"}, {"303", 1, "wrong_database"}} {
		r, db := identityFixture(t)
		db.facts[r.Logs()] = fact
		if _, err := storage.VerifyLogIsolation(t.Context(), db, r); err != storage.ErrLogIdentity {
			t.Fatal("malformed identity accepted", err)
		}
	}
	r, db := identityFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := storage.VerifyLogIsolation(ctx, db, r); err != storage.ErrLogIdentity || len(db.transactions) != 0 {
		t.Fatal("cancelled inspection opened database", err)
	}
}

func TestLogIdentityAllowsSharedClusterOnlyForDistinctLogDatabases(t *testing.T) {
	r, db := identityFixture(t)
	upstream, _ := r.LogConnection(storage.UpstreamLog)
	platform := db.facts[r.Logs()]
	fact := db.facts[upstream]
	fact.cluster = platform.cluster
	fact.oid = platform.oid + 1
	db.facts[upstream] = fact
	proof, err := storage.VerifyLogIsolation(t.Context(), db, r)
	if err != nil {
		t.Fatal("separate databases on shared log cluster rejected", err)
	}
	tx, _ := db.Begin(t.Context(), upstream)
	defer tx.Rollback(context.Background())
	if err := proof.Match(t.Context(), tx, upstream); err != nil {
		t.Fatal("shared-cluster proof cannot verify write target", err)
	}
	fact.oid = platform.oid
	db.facts[upstream] = fact
	if _, err := storage.VerifyLogIsolation(t.Context(), db, r); !errors.Is(err, storage.ErrLogIsolation) {
		t.Fatal("alias to same physical log database accepted", err)
	}
}

func TestControlAndLogsCanShareClusterButNeverPhysicalDatabase(t *testing.T) {
	r, db := identityFixture(t)
	// Model an alias during deployment: physical checks must not rely only on
	// the configured hostname when allowing control on the shared log cluster.
	db.facts[r.Control()] = identityFact{"303", 9, "sicp_control"}
	if _, err := storage.VerifyLogIsolation(t.Context(), db, r); err != nil {
		t.Fatal("distinct control on log cluster rejected", err)
	}
	for _, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		c, _ := r.LogConnection(source)
		before := db.facts[c]
		changed := before
		changed.cluster, changed.oid = "303", 9
		db.facts[c] = changed
		if _, err := storage.VerifyLogIsolation(t.Context(), db, r); !errors.Is(err, storage.ErrLogIsolation) {
			t.Fatal("log aliases relocated control database", err)
		}
		db.facts[c] = before
	}
}
