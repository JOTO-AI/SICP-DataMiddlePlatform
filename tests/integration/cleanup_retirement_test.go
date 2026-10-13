//go:build integration

package integration

import (
	"context"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"testing"
	"time"
)

func TestT34_04To06PostgresReferenceRetirementAndReceivedPrefix(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	control, logs := e.connection(t, 4), e.connection(t, 5)
	registry, err := e.registry.WithServiceLogAccess(true)
	if err != nil {
		t.Fatal(err)
	}
	repo := operations.Repository{Registry: registry, Database: e.db}
	m := operations.Maintenance{Repository: repo, LogDatabase: e.db, Owner: "maintenance"}
	now := time.Now().UTC()
	l := newLogs(t, "")
	m.Correlate = l.sanitizer.Correlation
	if err = repo.EnsureNotifications(e.ctx, "", false, now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"terminal", "still-referenced", "live"} {
		if _, err = control.Exec(e.ctx, `INSERT INTO control.tasks(id,idempotency_key,mode,status,requested_by,accepted_at,finished_at) VALUES($1,$1,'initialize','failed','synthetic',$2,$2)`, id, now); err != nil {
			t.Fatal(err)
		}
		if _, err = appendProtocol(e.ctx, logs, protocolEvent(t, l, id, events.Task)); err != nil {
			t.Fatal(err)
		}
	}
	if err = repo.NotificationFacts(e.ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err = control.Exec(e.ctx, `INSERT INTO control.notification_deliveries(id,fact_id,rule_id,revision,recipient,status,created_at,deadline) VALUES('busy','task:still-referenced','task_failed',1,'a@example.invalid','queued',$1,$2)`, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = logs.Exec(e.ctx, "UPDATE logs.events SET received_at=clock_timestamp()-interval '40 days'"); err != nil {
		t.Fatal(err)
	}
	p, err := m.Preview(e.ctx, storage.PlatformLog, "admin", now)
	if err != nil || p.Count != 1 || p.Through != 1 {
		t.Fatal("prefix jumped protected event", p, err)
	}
	var count int
	if err = control.QueryRow(e.ctx, "SELECT count(*) FROM control.log_retirements").Scan(&count); err != nil || count != 0 {
		t.Fatal("preview retired data", count, err)
	}
	if _, err = m.Accept(e.ctx, "other", "foreign", operations.CleanupRequest{PreviewID: p.ID, Revision: p.Revision}, now); err != operations.ErrMissing {
		t.Fatal("foreign preview", err)
	}
	if _, err = m.Accept(e.ctx, "admin", "expired", operations.CleanupRequest{PreviewID: p.ID, Revision: p.Revision}, now.Add(10*time.Minute)); err != operations.ErrConflict {
		t.Fatal("expired preview", err)
	}
	job, err := m.Accept(e.ctx, "admin", "first", operations.CleanupRequest{PreviewID: p.ID, Revision: p.Revision}, now)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := m.Accept(e.ctx, "admin", "first", operations.CleanupRequest{PreviewID: p.ID, Revision: p.Revision}, now)
	if err != nil || replay.ID != job.ID {
		t.Fatal("duplicate acceptance", err)
	}
	if err = m.Pump(e.ctx); err != nil {
		t.Fatal(err)
	}
	done, err := repo.Job(e.ctx, job.ID, "admin", "cleanup")
	if err != nil || done.Status != "succeeded" {
		t.Fatal(done, err)
	}
	var floor int64
	if err = logs.QueryRow(e.ctx, "SELECT floor FROM logs.commit_state WHERE singleton").Scan(&floor); err != nil || floor != 1 {
		t.Fatal(floor, err)
	}
	for _, sql := range []string{
		`UPDATE control.tasks SET status='running' WHERE id='terminal'`,
		`INSERT INTO control.checkpoints VALUES('terminal','late','{}',clock_timestamp())`,
		`INSERT INTO control.notification_deliveries(id,fact_id,rule_id,revision,recipient,status,created_at,deadline) VALUES('late','task:terminal','task_failed',1,'a@example.invalid','queued',clock_timestamp(),clock_timestamp()+interval '1 hour')`,
	} {
		if _, err = control.Exec(e.ctx, sql); err == nil {
			t.Fatal("retired dependency revived", sql)
		}
	}
	// Same event ID is acknowledged without recreating the removed payload.
	if n, err := appendProtocol(e.ctx, logs, protocolEvent(t, l, "terminal", events.Task)); err != nil || n != 1 {
		t.Fatal("receipt replay", n, err)
	}
	if err = logs.QueryRow(e.ctx, "SELECT count(*) FROM logs.events WHERE event_id=$1", l.sanitizer.Correlation("terminal")).Scan(&count); err != nil || count != 0 {
		t.Fatal("deleted payload resurrected", count, err)
	}
	// A live reference added AFTER preview wins over retirement at execution.
	if _, err = control.Exec(e.ctx, "UPDATE control.notification_deliveries SET status='cancelled' WHERE id='busy'"); err != nil {
		t.Fatal(err)
	}
	p, err = m.Preview(e.ctx, storage.PlatformLog, "admin", now)
	if err != nil || p.Count != 2 {
		t.Fatal(p, err)
	}
	if _, err = control.Exec(e.ctx, "UPDATE control.tasks SET status='running',finished_at=NULL WHERE id='live'"); err != nil {
		t.Fatal(err)
	}
	job, err = m.Accept(e.ctx, "admin", "raced", operations.CleanupRequest{PreviewID: p.ID, Revision: p.Revision}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Pump(e.ctx); err != nil {
		t.Fatal(err)
	}
	done, err = repo.Job(e.ctx, job.ID, "admin", "cleanup")
	if err != nil || done.Status != "partial_failed" {
		t.Fatal("reference race not refused", done, err)
	}
	if err = logs.QueryRow(e.ctx, "SELECT floor FROM logs.commit_state WHERE singleton").Scan(&floor); err != nil || floor != 1 {
		t.Fatal("race deleted data", floor, err)
	}
	// Occurred-at is old, but a late received record remains inside retention.
	if _, err = appendProtocol(e.ctx, logs, protocolEvent(t, l, "late-arrival", events.Request)); err != nil {
		t.Fatal(err)
	}
	if _, err = control.Exec(e.ctx, "UPDATE control.tasks SET status='failed',finished_at=clock_timestamp() WHERE id='live'"); err != nil {
		t.Fatal(err)
	}
	p, err = m.Preview(e.ctx, storage.PlatformLog, "admin", now)
	if err != nil || p.Count != 2 || p.Through != 3 {
		t.Fatal("late event removed by occurred time", p, err)
	}
}

func TestT34_05PostgresReferenceAdmissionWaitsForCommittedRetirement(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	owner, late, observer := e.connection(t, 4), e.connection(t, 4), e.connection(t, 4)
	if _, err := owner.Exec(e.ctx, `INSERT INTO control.tasks(id,idempotency_key,mode,status,requested_by,accepted_at,finished_at) VALUES('retire','retire','initialize','success_unchanged','synthetic',clock_timestamp(),clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	tx, err := owner.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(e.ctx, "SELECT pg_advisory_xact_lock(734219120)"); err != nil {
		t.Fatal(err)
	}
	var eligible bool
	if err = tx.QueryRow(e.ctx, "SELECT control.log_reference_eligible('task:retire')").Scan(&eligible); err != nil || !eligible {
		t.Fatal(eligible, err)
	}
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		_, err := late.Exec(ctx, "INSERT INTO control.checkpoints VALUES('retire','late','{}',clock_timestamp())")
		result <- err
	}()
	defer func() { cancel(); <-joined }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err = observer.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')", late.PgConn().PID()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("no reference barrier")
		case err := <-result:
			t.Fatal("admission passed retirement", err)
		case <-ticker.C:
		}
	}
	if _, err = tx.Exec(e.ctx, `SELECT control.retire_log_references('["task:retire"]'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err == nil {
		t.Fatal("waiting reference admitted after retirement")
	}
}

func TestT34_04PostgresRetirementProtectsEveryRegisteredDependency(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	c := e.connection(t, 4)
	for _, kind := range []string{"active", "reader", "snapshot", "resource", "backfill", "recovery", "outbox", "execution", "publication"} {
		t.Run(kind, func(t *testing.T) {
			// Roll back each independent protection fixture, including shared slot rows.
			tx, err := c.Begin(e.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			exec := func(sql string) {
				t.Helper()
				if _, err := tx.Exec(e.ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO control.tasks(id,idempotency_key,mode,status,requested_by,accepted_at,finished_at) VALUES('protected','protected','initialize','success_unchanged','synthetic',clock_timestamp(),clock_timestamp())`)
			exec(`INSERT INTO control.bundles(id,tables,created_at) VALUES('bundle','{}',clock_timestamp())`)
			exec(`INSERT INTO control.candidates(id,task_id,instance,slot,generation,bundle_id,owner_generation,state) VALUES('candidate','protected','M',0,1,'bundle',1,'ready')`)
			switch kind {
			case "active":
				exec(`UPDATE control.slots SET bundle_id='bundle',admission_open=true WHERE instance='M' AND slot=0`)
			case "reader":
				exec(`INSERT INTO control.readers(id,instance,slot,generation,bundle_id,process_id,admitted_at) VALUES('reader','M',0,1,'bundle','process',clock_timestamp())`)
			case "snapshot":
				exec(`INSERT INTO control.query_snapshots VALUES('snapshot','bundle','operation','{}',clock_timestamp(),clock_timestamp()+interval '1 minute')`)
			case "resource":
				exec(`INSERT INTO control.resources(id,kind,bundle_id) VALUES('resource','version','bundle')`)
			case "backfill", "recovery":
				exec(`INSERT INTO control.resources(id,kind,bundle_id,retired) VALUES('resource','version','bundle',true)`)
				if _, err = tx.Exec(e.ctx, `INSERT INTO control.resource_refs VALUES('resource','owner',$1,false)`, kind); err != nil {
					t.Fatal(err)
				}
			case "outbox":
				exec(`INSERT INTO control.notification_outbox(id,failure_key,task_id,payload,created_at) VALUES('outbox','failure','protected','{}',clock_timestamp())`)
			case "execution":
				exec(`UPDATE control.execution SET task_id='protected',owner_id='worker' WHERE singleton`)
			case "publication":
				exec(`INSERT INTO control.publication_outbox(id,task_id,kind,target,created_at) VALUES('publication','protected','bundle_publish','{}',clock_timestamp())`)
			}
			var eligible bool
			if err = tx.QueryRow(e.ctx, `SELECT control.log_reference_eligible('task:protected')`).Scan(&eligible); err != nil || eligible {
				t.Fatal("dependency ignored", kind, eligible, err)
			}
		})
	}
}

func TestT34_05PostgresOldSnapshotCannotRegisterRetiredReference(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	old, retirer := e.connection(t, 4), e.connection(t, 4)
	if _, err := old.Exec(e.ctx, `INSERT INTO control.tasks(id,idempotency_key,mode,status,requested_by,accepted_at,finished_at) VALUES('old','old','initialize','success_unchanged','synthetic',clock_timestamp(),clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	tx, err := old.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(e.ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(e.ctx, "SELECT count(*) FROM control.log_retirements").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if _, err = retirer.Exec(e.ctx, `SELECT control.retire_log_references('["task:old"]'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(e.ctx, `INSERT INTO control.checkpoints VALUES('old','late','{}',clock_timestamp())`); err == nil {
		t.Fatal("old snapshot bypassed permanent retirement")
	}
}
