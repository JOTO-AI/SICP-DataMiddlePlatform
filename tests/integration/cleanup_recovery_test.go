//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"strings"
	"testing"
	"time"
)

// Fault injection wraps real transactions and fails only the named boundary.
type cleanupFaultDB struct {
	storage.Database
	fail string
}

func (d *cleanupFaultDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.Database.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return &cleanupFaultTx{Transaction: tx, db: d}, nil
}

type cleanupFaultTx struct {
	storage.Transaction
	db *cleanupFaultDB
}

func (tx *cleanupFaultTx) Exec(ctx context.Context, q string, a ...any) (int64, error) {
	if tx.db.fail != "" && strings.Contains(q, tx.db.fail) {
		return 0, errors.New("synthetic control outage with private DSN")
	}
	return tx.Transaction.Exec(ctx, q, a...)
}
func (tx *cleanupFaultTx) Query(ctx context.Context, q string, a ...any) (storage.Rows, error) {
	return tx.Transaction.(storage.QueryExecutor).Query(ctx, q, a...)
}

func TestT34_12And13PostgresDeleteCommitThenControlFailureRecoversOnce(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	c, l := e.connection(t, 4), e.connection(t, 5)
	registry, err := e.registry.WithServiceLogAccess(true)
	if err != nil {
		t.Fatal(err)
	}
	fault := &cleanupFaultDB{Database: e.db}
	repo := operations.Repository{Registry: registry, Database: fault}
	m := operations.Maintenance{Repository: repo, LogDatabase: e.db, Owner: "first"}
	fixture := newLogs(t, "")
	if _, err = appendProtocol(e.ctx, l, protocolEvent(t, fixture, "cleanup", events.Request)); err != nil {
		t.Fatal(err)
	}
	if _, err = l.Exec(e.ctx, "UPDATE logs.events SET received_at=clock_timestamp()-interval '40 days'"); err != nil {
		t.Fatal(err)
	}
	p, err := m.Preview(e.ctx, storage.PlatformLog, "trusted-admin", time.Now())
	if err != nil || p.Count != 1 {
		t.Fatal(p, err)
	}
	// Admission and its audit commit together. A failed audit cannot return 202.
	fault.fail = "INSERT INTO control.management_audit"
	request := operations.CleanupRequest{PreviewID: p.ID, Revision: p.Revision}
	if _, err = m.Accept(e.ctx, "trusted-admin", "cleanup", request, time.Now()); err == nil {
		t.Fatal("accepted without durable audit")
	}
	var count int
	if err = c.QueryRow(e.ctx, "SELECT count(*) FROM control.management_jobs").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial acceptance", count, err)
	}
	fault.fail = ""
	job, err := m.Accept(e.ctx, "trusted-admin", "cleanup", request, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// The client leaves after durable acceptance. Worker execution is independent.
	client, cancel := context.WithCancel(e.ctx)
	cancel()
	if _, err = repo.Job(client, job.ID, "trusted-admin", "cleanup"); err == nil {
		t.Fatal("cancelled read succeeded")
	}
	fault.fail = "status=$3,summary="
	if err = m.Pump(e.ctx); err == nil {
		t.Fatal("writeback failure hidden")
	}
	if err = l.QueryRow(e.ctx, "SELECT count(*) FROM logs.cleanup_receipts").Scan(&count); err != nil || count != 1 {
		t.Fatal("delete not committed before injected failure", count, err)
	}
	fault.fail = ""
	if _, err = c.Exec(e.ctx, "UPDATE control.management_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID); err != nil {
		t.Fatal(err)
	}
	// Change policy and expire preview after the original delete: receipt wins.
	if _, err = c.Exec(e.ctx, "UPDATE control.cleanup_previews SET expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveRetention(e.ctx, operations.Retention{Revision: 1, PlatformDays: 90, UpstreamDays: 90}, "trusted-admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	m.Owner = "restarted"
	if err = m.Pump(e.ctx); err != nil {
		t.Fatal(err)
	}
	done, err := repo.Job(e.ctx, job.ID, "trusted-admin", "cleanup")
	if err != nil || done.Status != "succeeded" {
		t.Fatal(done, err)
	}
	var summary struct {
		Deleted int64
		Known   bool `json:"outcome_known"`
	}
	if err = json.Unmarshal(done.Summary, &summary); err != nil || summary.Deleted != 1 || !summary.Known {
		t.Fatal("lost actual count", string(done.Summary), err)
	}
	if err = m.Pump(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err = l.QueryRow(e.ctx, "SELECT count(*) FROM logs.cleanup_receipts").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate deletion", count, err)
	}
	notifier := operations.Notifier{Repository: repo, Owner: "audit", Audit: events.SinkFunc(func(context.Context, events.Event) error { return errors.New("log unavailable") })}
	if err = notifier.Pump(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err = c.QueryRow(e.ctx, "SELECT count(*) FROM control.management_audit WHERE NOT confirmed").Scan(&count); err != nil || count != 2 {
		t.Fatal("lost pending audit", count, err)
	}
	notifier.Audit = events.SinkFunc(func(_ context.Context, v events.Event) error {
		if v.Audit == nil || v.Audit.ActorID == "trusted-admin" {
			t.Fatal("audit identity not redacted")
		}
		return nil
	})
	if err = notifier.Pump(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err = c.QueryRow(e.ctx, "SELECT count(*) FROM control.management_audit WHERE NOT confirmed").Scan(&count); err != nil || count != 0 {
		t.Fatal("audit recovery failed", count, err)
	}
}
