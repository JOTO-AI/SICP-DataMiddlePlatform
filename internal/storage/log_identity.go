package storage

import (
	"context"
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
// It must not be persisted or reused after reconnect/reconfiguration. Runtime
// per-source verification and degraded startup are assembled in P28.03.
type LogIsolation struct {
	identities map[Connection]databaseIdentity
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
	// Even different databases on the same server violate the independent log
	// instance requirement. Cloned clusters with the same system ID fail closed.
	for _, logTarget := range targets[5:] {
		for _, other := range targets {
			if other != logTarget && proof.identities[other].cluster == proof.identities[logTarget].cluster {
				return LogIsolation{}, ErrLogIsolation
			}
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
