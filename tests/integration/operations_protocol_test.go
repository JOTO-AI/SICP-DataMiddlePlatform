//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These exercise actual PostgreSQL transactions and privileges. They are
// narrower protocol regressions, not replacements for the full OI scenarios.
func protocolEvent(t *testing.T, l *logFixture, id string, kind events.Kind) observability.SafeEvent {
	t.Helper()
	e := events.Event{ID: id, EntityID: id, Kind: kind, Phase: events.Started, OccurredAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}
	if kind == events.Task {
		e.Identity.TaskID = id
	} else {
		e.Identity.RequestID = id
	}
	safe, err := l.sanitizer.Sanitize(e, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	return safe
}

func appendProtocol(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, safe observability.SafeEvent) (int64, error) {
	e := safe.Event()
	var n int64
	err := q.QueryRow(ctx, "SELECT logs.append_event($1,$2,$3,$4,$5,$6::jsonb)", e.ID, string(e.Kind), e.EntityID, string(e.Phase), e.OccurredAt, string(safe.Bytes())).Scan(&n)
	return n, err
}

func TestT29_PostgresCounterCannotExposeLaterCommitPastPendingWriter(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	l := newLogs(t, "")
	one, two, observer := e.connection(t, 5), e.connection(t, 5), e.connection(t, 5)
	first, err := one.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(context.Background())
	if n, err := appendProtocol(e.ctx, first, protocolEvent(t, l, "first", events.Request)); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	second, err := two.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback(context.Background())
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	completed := make(chan error, 1)
	safe := protocolEvent(t, l, "second", events.Request)
	go func() {
		n, err := appendProtocol(ctx, second, safe)
		if err == nil && n != 2 {
			err = errors.New("unexpected allocation")
		}
		if err == nil {
			err = second.Commit(ctx)
		}
		completed <- err
	}()
	// Wait for PostgreSQL's own lock observation, not a timing assumption.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := observer.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')", two.PgConn().PID()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-completed:
			t.Fatalf("second writer escaped pending commit: %v", err)
		case <-ctx.Done():
			t.Fatal("writer lock not observed")
		case <-ticker.C:
		}
	}
	var high, count int64
	if err := observer.QueryRow(e.ctx, "SELECT high_water,(SELECT count(*) FROM logs.events) FROM logs.commit_state").Scan(&high, &count); err != nil || high != 0 || count != 0 {
		t.Fatal("uncommitted high-water exposed", high, count, err)
	}
	if err := first.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if err := observer.QueryRow(e.ctx, "SELECT high_water,(SELECT count(*) FROM logs.events) FROM logs.commit_state").Scan(&high, &count); err != nil || high != 2 || count != 2 {
		t.Fatal(high, count, err)
	}
	rolled, err := one.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := appendProtocol(e.ctx, rolled, protocolEvent(t, l, "rolled", events.Request)); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if err := rolled.Rollback(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := appendProtocol(e.ctx, one, protocolEvent(t, l, "after-rollback", events.Request)); err != nil || n != 3 {
		t.Fatal("rollback consumed committed position", n, err)
	}
}

func TestT34_PostgresCleanupKeepsProtectedPrefixAndPermanentReplayReceipt(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	l := newLogs(t, "")
	conn := e.connection(t, 5)
	items := []observability.SafeEvent{protocolEvent(t, l, "old-request", events.Request), protocolEvent(t, l, "protected-task", events.Task), protocolEvent(t, l, "later-request", events.Request)}
	for i, safe := range items {
		if n, err := appendProtocol(e.ctx, conn, safe); err != nil || n != int64(i+1) {
			t.Fatal(n, err)
		}
	}
	if _, err := conn.Exec(e.ctx, "UPDATE logs.events SET received_at=clock_timestamp()-interval '40 days'"); err != nil {
		t.Fatal(err)
	}
	var epoch string
	var floor, through, count int64
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	if err := conn.QueryRow(e.ctx, "SELECT epoch,floor,through,count FROM logs.cleanup_preview($1)", cutoff).Scan(&epoch, &floor, &through, &count); err != nil || floor != 0 || through != 1 || count != 1 {
		t.Fatal("protected row did not stop prefix", floor, through, count, err)
	}
	for range 2 {
		var removed int64
		if err := conn.QueryRow(e.ctx, "SELECT logs.cleanup_prefix('synthetic-cleanup',$1,$2,$3,$4)", epoch, floor, through, cutoff).Scan(&removed); err != nil || removed != 1 {
			t.Fatal("cleanup receipt replay", removed, err)
		}
	}
	if err := conn.QueryRow(e.ctx, "SELECT floor,(SELECT count(*) FROM logs.events) FROM logs.commit_state").Scan(&floor, &count); err != nil || floor != 1 || count != 2 {
		t.Fatal("delete and floor disagree", floor, count, err)
	}
	if n, err := appendProtocol(e.ctx, conn, items[0]); err != nil || n != 1 {
		t.Fatal("receipt not replayable", n, err)
	}
	if err := conn.QueryRow(e.ctx, "SELECT count(*) FROM logs.events WHERE event_id=$1", items[0].ID()).Scan(&count); err != nil || count != 0 {
		t.Fatal("cleaned payload resurrected", count, err)
	}
	// Same id, different content stays a conflict after payload retirement.
	var conflict int64
	event := items[0].Event()
	changed := event.OccurredAt.Add(time.Second)
	if err := conn.QueryRow(e.ctx, `SELECT logs.append_event($1,$2,$3,$4,$5,jsonb_set($6::jsonb,'{event,occurred_at}',to_jsonb($7::text)))`, event.ID, string(event.Kind), event.EntityID, string(event.Phase), changed, string(items[0].Bytes()), changed.Format(time.RFC3339Nano)).Scan(&conflict); err != nil || conflict != -1 {
		t.Fatal("lost retained conflict marker", conflict, err)
	}
}

func TestT29_PostgresRolesRejectWriterBypassAndReaderMutation(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	l := newLogs(t, "")
	conn := e.connection(t, 5)
	if _, err := conn.Exec(e.ctx, "SET ROLE sicp_logger"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(context.Background(), "RESET ROLE")
	if _, err := conn.Exec(e.ctx, "DELETE FROM logs.events"); err == nil {
		t.Fatal("dispatcher can delete")
	}
	if _, err := conn.Exec(e.ctx, "INSERT INTO logs.events(event_id,kind,entity_id,phase,occurred_at,payload,sequence) VALUES('bypass','request','bypass','started',now(),'{}',999)"); err == nil {
		t.Fatal("old direct writer still permitted")
	}
	if _, err := appendProtocol(e.ctx, conn, protocolEvent(t, l, "allowed", events.Request)); err != nil {
		t.Fatal("supported writer rejected", err)
	}
	if _, err := conn.Exec(e.ctx, "RESET ROLE; SET ROLE sicp_log_reader"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := conn.QueryRow(e.ctx, "SELECT count(*) FROM logs.events").Scan(&count); err != nil || count != 1 {
		t.Fatal("reader cannot read", err)
	}
	if _, err := appendProtocol(e.ctx, conn, protocolEvent(t, l, "reader-write", events.Request)); err == nil {
		t.Fatal("reader can execute write function")
	}
}

func TestT34_PostgresCleanupRecoveryWaitsForUncertainDeletion(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	l := newLogs(t, "")
	writer, recovery, observer := e.connection(t, 5), e.connection(t, 5), e.connection(t, 5)
	if _, err := appendProtocol(e.ctx, writer, protocolEvent(t, l, "retained-receipt", events.Request)); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(e.ctx, "UPDATE logs.events SET received_at=clock_timestamp()-interval '40 days'"); err != nil {
		t.Fatal(err)
	}
	var epoch string
	if err := writer.QueryRow(e.ctx, "SELECT epoch FROM logs.commit_state WHERE singleton").Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	tx, err := writer.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var removed int64
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	if err := tx.QueryRow(e.ctx, "SELECT logs.cleanup_try_prefix('uncertain-job',$1,0,1,$2)", epoch, cutoff).Scan(&removed); err != nil || removed != 1 {
		t.Fatal(removed, err)
	}
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	type result struct {
		count *int64
		err   error
	}
	done := make(chan result, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		var out result
		out.err = recovery.QueryRow(ctx, "SELECT logs.cleanup_result('uncertain-job',$1,1)", epoch).Scan(&out.count)
		done <- out
	}()
	defer func() { cancel(); <-joined }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := observer.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')", recovery.PgConn().PID()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case out := <-done:
			t.Fatalf("recovery observed an unsettled transaction: %v", out.err)
		case <-ctx.Done():
			t.Fatal("recovery lock not observed")
		case <-ticker.C:
		}
	}
	if err := tx.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}
	out := <-done
	if out.err != nil || out.count == nil || *out.count != 1 {
		t.Fatal("committed deletion lost", out.err)
	}
	if err := observer.QueryRow(e.ctx, "SELECT logs.cleanup_try_prefix('stale-job',$1,0,1,$2)", epoch, cutoff).Scan(&removed); err != nil || removed != -1 {
		t.Fatal("stale preview not a known refusal", removed, err)
	}
	var count, floor int64
	if err := observer.QueryRow(e.ctx, "SELECT floor,(SELECT count(*) FROM logs.cleanup_receipts) FROM logs.commit_state WHERE singleton").Scan(&floor, &count); err != nil || floor != 1 || count != 1 {
		t.Fatal("refusal changed deletion ledger", floor, count, err)
	}
}

func TestI02_LogRepositoryNanosecondTimestamps(t *testing.T) {
	e := newEnvironmentMode(t, 3*time.Minute, "", true)
	e.migrate(t)
	l := newLogs(t, "")
	for i, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		t.Run(string(source), func(t *testing.T) {
			conn := e.connection(t, 5+i)
			store := observability.Repository{Registry: e.registry, Database: storage.VerifiedLogs{Registry: e.registry, Database: e.db}, Source: source}
			for _, ns := range []int{0, 123456000, 123456499, 123456500, 123456501, 123456789, 999999999} {
				raw := events.Event{ID: fmt.Sprintf("event-%d", ns), EntityID: "entity", Kind: events.Request, Phase: events.Started, OccurredAt: time.Date(2026, 10, 13, 23, 59, 59, ns, time.UTC)}
				raw.Identity.RequestID = "entity"
				if source == storage.UpstreamLog {
					raw.Kind = events.Call
					raw.Identity.Purpose, raw.Identity.TestRunID = events.TestPurpose, "test-run"
				}
				safe, err := l.sanitizer.Sanitize(raw, observability.Details{})
				if err != nil {
					t.Fatal(err)
				}
				original := string(safe.Bytes())
				// Reproduce the production failure with the old binary timestamp
				// parameter before exercising the repository's text conversion.
				if ns == 123456789 {
					var sequence int64
					err := conn.QueryRow(e.ctx, "SELECT logs.append_event($1,$2,$3,$4,$5,$6::jsonb)", safe.ID(), string(raw.Kind), safe.Event().EntityID, string(raw.Phase), raw.OccurredAt, original).Scan(&sequence)
					var pgErr *pgconn.PgError
					if !errors.As(err, &pgErr) || pgErr.Code != "P0001" || pgErr.Message != "invalid event" {
						t.Fatal("old timestamp binding did not reproduce invalid event", err)
					}
				}
				safe, err = l.sanitizer.Restore(safe.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				for replay := 0; replay < 2; replay++ {
					if err := store.StoreBatch(e.ctx, []observability.SafeEvent{safe}); err != nil {
						t.Fatalf("nanoseconds=%d replay=%d: %v", ns, replay, err)
					}
				}
				var intact bool
				if err := conn.QueryRow(e.ctx, `SELECT payload=$2::jsonb AND occurred_at=(payload #>> '{event,occurred_at}')::timestamptz FROM logs.events WHERE event_id=$1`, safe.ID(), original).Scan(&intact); err != nil || !intact {
					t.Fatal("payload changed or timestamp validation disagrees", err)
				}
			}
			var high, count int64
			if err := conn.QueryRow(e.ctx, "SELECT high_water,(SELECT count(*) FROM logs.events) FROM logs.commit_state WHERE singleton").Scan(&high, &count); err != nil || high != 7 || count != 7 {
				t.Fatal("replay advanced sequence or duplicated events", high, count, err)
			}
		})
	}
}
