// Package migrate applies immutable migrations inside a per-database transaction.
package migrate

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

//go:embed sql/*.sql
var scripts embed.FS

type Domain string

const (
	Business Domain = "business"
	Control  Domain = "control"
	Logs     Domain = "logs"
)

type Target struct {
	Domain   Domain
	Position storage.Position
}

func (t Target) connection(r storage.Registry) (storage.Connection, error) {
	if t.Domain == Business {
		return r.Business(t.Position)
	}
	if t.Position != (storage.Position{}) {
		return storage.Connection{}, fmt.Errorf("fixed database cannot have a business position")
	}
	var c storage.Connection
	switch t.Domain {
	case Control:
		c = r.Control()
	case Logs:
		c = r.Logs()
	default:
		return c, fmt.Errorf("unknown migration domain")
	}
	if c.DSN() == "" {
		return c, fmt.Errorf("database registry not configured")
	}
	return c, nil
}

type Migration struct{ Version, SQL string }

func (m Migration) Checksum() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(m.SQL))) }

func Catalog(domain Domain) ([]Migration, error) {
	if domain != Business && domain != Control && domain != Logs {
		return nil, fmt.Errorf("unknown migration domain")
	}
	versions := []string{string(domain) + "_001"}
	if domain == Logs {
		versions = append(versions, "logs_002")
	}
	if domain == Business {
		versions = append(versions, "business_002")
	}
	if domain == Control {
		versions = append(versions, "control_002", "control_003", "control_004", "control_005", "control_006")
	}
	plan := make([]Migration, 0, len(versions))
	for _, version := range versions {
		data, err := scripts.ReadFile("sql/" + version + ".sql")
		if err != nil {
			return nil, err
		}
		plan = append(plan, Migration{Version: version, SQL: string(data)})
	}
	return plan, nil
}

// Apply validates the complete plan before opening a transaction. The advisory
// lock serializes migrations for this database; ledger and DDL commit together.
// On an ambiguous commit response the caller may safely retry the same plan.
func Apply(ctx context.Context, db storage.Database, registry storage.Registry, target Target, plan []Migration) (applied int, err error) {
	connection, err := target.connection(registry)
	if err != nil {
		return 0, err
	}
	last := ""
	for _, m := range plan {
		if !strings.HasPrefix(m.Version, string(target.Domain)+"_") || m.Version <= last || strings.TrimSpace(m.SQL) == "" {
			return 0, fmt.Errorf("invalid migration plan")
		}
		last = m.Version
	}
	tx, err := db.Begin(ctx, connection)
	if err != nil {
		return 0, fmt.Errorf("begin migration: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	for _, statement := range []string{
		"SELECT pg_advisory_xact_lock(734219001)",
		"CREATE SCHEMA IF NOT EXISTS migration_meta",
		"REVOKE ALL ON SCHEMA migration_meta FROM PUBLIC",
		"CREATE TABLE IF NOT EXISTS migration_meta.versions (version TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp())",
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return 0, fmt.Errorf("prepare migration ledger: %w", err)
		}
	}
	for _, m := range plan {
		var previous *string
		if err := tx.QueryRow(ctx, "SELECT (SELECT checksum FROM migration_meta.versions WHERE version=$1)", m.Version).Scan(&previous); err != nil {
			return 0, fmt.Errorf("read migration ledger: %w", err)
		}
		if previous != nil {
			if *previous != m.Checksum() {
				return 0, fmt.Errorf("migration checksum conflict")
			}
			continue
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return 0, fmt.Errorf("execute migration: %w", err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO migration_meta.versions (version, checksum) VALUES ($1,$2)", m.Version, m.Checksum()); err != nil {
			return 0, fmt.Errorf("record migration: %w", err)
		}
		applied++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit migration: %w", err)
	}
	return applied, nil
}
