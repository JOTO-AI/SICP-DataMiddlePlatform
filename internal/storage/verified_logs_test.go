package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type verifiedDB struct {
	registry     storage.Registry
	fail         storage.Connection
	invalid      storage.Connection
	transactions []*verifiedTx
}
type verifiedTx struct {
	db     *verifiedDB
	c      storage.Connection
	closed bool
}
type verifiedRow struct {
	valid bool
	err   error
}

func (r verifiedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.valid
	return nil
}
func (d *verifiedDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	if c == d.fail || ctx.Err() != nil {
		return nil, errors.New("secret-dsn")
	}
	tx := &verifiedTx{db: d, c: c}
	d.transactions = append(d.transactions, tx)
	return tx, nil
}
func (t *verifiedTx) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	source := storage.PlatformLog
	u, _ := t.db.registry.LogConnection(storage.UpstreamLog)
	if t.c == u {
		source = storage.UpstreamLog
	}
	if sql != "SELECT logs.verify_deployment($1,$2)" || len(args) != 2 || args[0] != string(source) || args[1] != t.db.registry.LogTopology() {
		return verifiedRow{err: errors.New("unbound verification")}
	}
	return verifiedRow{valid: t.c != t.db.invalid}
}
func (t *verifiedTx) Exec(context.Context, string, ...any) (int64, error) {
	return 0, errors.New("not expected")
}
func (t *verifiedTx) Commit(context.Context) error   { t.closed = true; return nil }
func (t *verifiedTx) Rollback(context.Context) error { t.closed = true; return nil }

func TestT28_11RuntimeIdentityGateIsPerSourceAndRechecksRecovery(t *testing.T) {
	r, err := load(dualValues())
	if err != nil {
		t.Fatal(err)
	}
	u, _ := r.LogConnection(storage.UpstreamLog)
	db := &verifiedDB{registry: r, fail: u}
	gate := storage.VerifiedLogs{Registry: r, Database: db}
	if !gate.Ready(t.Context(), storage.PlatformLog) || gate.Ready(t.Context(), storage.UpstreamLog) {
		t.Fatal("source health coupled")
	}
	db.fail = storage.Connection{}
	db.invalid = u
	if gate.Ready(t.Context(), storage.UpstreamLog) {
		t.Fatal("reachable unverified backend enabled")
	}
	db.invalid = storage.Connection{}
	if !gate.Ready(t.Context(), storage.UpstreamLog) {
		t.Fatal("recovery not verified")
	}
	db.invalid = u
	if tx, err := gate.Begin(t.Context(), u); err != storage.ErrLogIdentity || tx != nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("reconnected wrong backend enabled")
	}
	if tx, err := gate.Begin(t.Context(), r.Control()); err == nil || tx != nil {
		t.Fatal("control target admitted as logs")
	}
	for _, tx := range db.transactions {
		if !tx.closed {
			t.Fatal("verification transaction leaked")
		}
	}
}

func TestT28_11TopologyDigestExcludesCredentialsButBindsEveryTarget(t *testing.T) {
	v := dualValues()
	r, err := load(v)
	if err != nil {
		t.Fatal(err)
	}
	original := r.LogTopology()
	for k, value := range v {
		if strings.HasPrefix(k, "DATABASE_") && strings.HasSuffix(k, "_URL") {
			v[k] = strings.Replace(value, "postgres://", "postgres://rotated:synthetic@", 1)
			parts := strings.Split(v[k], "@")
			if len(parts) > 2 {
				v[k] = parts[0] + "@" + parts[len(parts)-1]
			}
		}
	}
	r, err = load(v)
	if err != nil {
		t.Fatal(err)
	}
	if r.LogTopology() != original {
		t.Fatal("credential rotation changed topology")
	}
	v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://rotated@new-upstream:5432/logs?sslmode=disable"
	r, err = load(v)
	if err != nil {
		t.Fatal(err)
	}
	if r.LogTopology() == original {
		t.Fatal("new target retained attestation")
	}
}
