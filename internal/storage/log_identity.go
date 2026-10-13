package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

var ErrLogIdentity = errors.New("log database identity unavailable or unverified")
var ErrLogIsolation = errors.New("log database isolation check failed")

// PostgreSQL cluster identity survives hostname aliases. Database OIDs are only
// unique within a cluster; both parts are required. Use a provisioning account
// allowed to call pg_control_system(), never grant the writer superuser access.
const logIdentitySQL = `SELECT s.system_identifier::text, d.oid::bigint, d.datname
FROM pg_catalog.pg_control_system() AS s
CROSS JOIN pg_catalog.pg_database AS d
WHERE d.datname = pg_catalog.current_database()`

type databaseIdentity struct {
	cluster string
	oid     int64
	name    string
}

func readDatabaseIdentity(ctx context.Context, executor Executor, c Connection) (databaseIdentity, error) {
	var id databaseIdentity
	if executor == nil || c.dsn == "" || ctx.Err() != nil {
		return id, ErrLogIdentity
	}
	if err := executor.QueryRow(ctx, logIdentitySQL).Scan(&id.cluster, &id.oid, &id.name); err != nil {
		return databaseIdentity{}, ErrLogIdentity
	}
	n, err := strconv.ParseUint(id.cluster, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != id.cluster || id.oid < 1 || id.oid > 1<<32-1 || id.name != c.database || ctx.Err() != nil {
		return databaseIdentity{}, ErrLogIdentity
	}
	return id, nil
}

// LogIsolation is a short-lived deployment proof, not a service readiness flag.
// It must not be reused after reconnect/reconfiguration. Attest rechecks the
// current transaction before creating a separate deployment identity record.
type LogIsolation struct {
	identities map[Connection]databaseIdentity
}

// LogTopology excludes credentials, so credential rotation cannot invalidate
// deployment identity. Changing any configured target requires new preflight.
func (r Registry) LogTopology() string {
	var targets []string
	for _, instance := range []Instance{Main, Standby} {
		for slot := 0; slot < 2; slot++ {
			c, e := r.Business(Position{Instance: instance, Slot: slot})
			if e != nil {
				return ""
			}
			targets = append(targets, c.endpoint, c.database)
		}
	}
	for _, c := range []Connection{r.control, r.logs, r.upstreamLogs} {
		targets = append(targets, c.endpoint, c.database)
	}
	body, _ := json.Marshal(targets)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func (r Registry) LogTarget(source LogSource) string {
	c, err := r.LogConnection(source)
	if err != nil {
		return ""
	}
	body, _ := json.Marshal([]string{c.endpoint, c.database})
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

// Attest is called by the provisioning migration transaction after DDL. Writers
// cannot alter this record or execute pg_control_system themselves. Runtime
// checks a narrowly granted SECURITY DEFINER function on each acquired backend.
func (v LogIsolation) Attest(ctx context.Context, tx Executor, r Registry, source LogSource) error {
	c, err := r.LogConnection(source)
	if err != nil {
		return ErrLogIdentity
	}
	if err = v.Match(ctx, tx, c); err != nil {
		return err
	}
	id := v.identities[c]
	_, err = tx.Exec(ctx, `INSERT INTO logs.deployment_identity (singleton,source,cluster_id,database_oid,topology) VALUES (true,$1,$2,$3,$4)
ON CONFLICT (singleton) DO UPDATE SET source=EXCLUDED.source,cluster_id=EXCLUDED.cluster_id,database_oid=EXCLUDED.database_oid,topology=EXCLUDED.topology`, string(source), id.cluster, id.oid, r.LogTopology())
	if err != nil {
		return ErrLogIdentity
	}
	return nil
}

func (LogIsolation) String() string     { return "[log database identities redacted]" }
func (v LogIsolation) GoString() string { return v.String() }

// Match rechecks the selected backend inside the migration transaction, before
// any DDL. It refuses routing changes between the probe and the actual write.
func (v LogIsolation) Match(ctx context.Context, executor Executor, c Connection) error {
	expected, ok := v.identities[c]
	if !ok {
		return ErrLogIdentity
	}
	actual, err := readDatabaseIdentity(ctx, executor, c)
	if err != nil {
		return err
	}
	if actual != expected {
		return ErrLogIsolation
	}
	return nil
}

// VerifyLogIsolation is a bounded, read-only deployment preflight. All targets
// must be known before a dual-log migration; unavailable is not verified. This
// all-target preflight is NOT a runtime health probe for either log pipeline.
func VerifyLogIsolation(ctx context.Context, db Database, r Registry) (LogIsolation, error) {
	if db == nil || (r.logMode != SingleLegacy && r.logMode != DualLogs) {
		return LogIsolation{}, ErrLogIdentity
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var targets []Connection
	for _, instance := range []Instance{Main, Standby} {
		for slot := 0; slot < 2; slot++ {
			c, err := r.Business(Position{Instance: instance, Slot: slot})
			if err != nil {
				return LogIsolation{}, ErrLogIdentity
			}
			targets = append(targets, c)
		}
	}
	targets = append(targets, r.control, r.logs)
	if r.logMode == DualLogs {
		targets = append(targets, r.upstreamLogs)
	}
	proof := LogIsolation{identities: make(map[Connection]databaseIdentity, len(targets))}
	for _, c := range targets {
		id, err := probeDatabaseIdentity(ctx, db, c)
		if err != nil {
			return LogIsolation{}, err
		}
		proof.identities[c] = id
	}
	// Logs remain on an instance separate from M/S. The two log sources may
	// share a cluster, but never the same physical database (including aliases).
	for _, logTarget := range targets[5:] {
		for _, other := range targets[:5] {
			if proof.identities[other].cluster == proof.identities[logTarget].cluster {
				return LogIsolation{}, ErrLogIsolation
			}
		}
	}
	if r.logMode == DualLogs {
		platform, upstream := proof.identities[r.logs], proof.identities[r.upstreamLogs]
		if platform.cluster == upstream.cluster && (platform.oid == upstream.oid || platform.name == upstream.name) {
			return LogIsolation{}, ErrLogIsolation
		}
	}
	return proof, nil
}

func probeDatabaseIdentity(ctx context.Context, db Database, c Connection) (id databaseIdentity, err error) {
	if c.dsn == "" || ctx.Err() != nil {
		return id, ErrLogIdentity
	}
	tx, err := db.Begin(ctx, c)
	if err != nil {
		return id, ErrLogIdentity
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if tx.Rollback(cleanup) != nil {
			id = databaseIdentity{}
			err = ErrLogIdentity
		}
	}()
	if _, err = tx.Exec(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		return id, ErrLogIdentity
	}
	return readDatabaseIdentity(ctx, tx, c)
}
