package observability_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/jackc/pgx/v5/pgtype"
)

type logsDB struct {
	rows        map[string]string
	lostReply   bool
	connections []string
	checkAppend func(string, []any)
}
type logsTx struct {
	db      *logsDB
	pending map[string]string
}

func (d *logsDB) Begin(_ context.Context, c storage.Connection) (storage.Transaction, error) {
	d.connections = append(d.connections, c.DSN())
	pending := map[string]string{}
	for k, v := range d.rows {
		pending[k] = v
	}
	return &logsTx{d, pending}, nil
}
func (t *logsTx) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	// The migrated logger cannot insert directly, including during cutover.
	return 0, errors.New("direct log writes forbidden")
}

type logRow struct{ body string }

type appendRow struct{ sequence int64 }

func (r appendRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("bad append destinations")
	}
	*dest[0].(*int64) = r.sequence
	return nil
}

func (r logRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("bad destinations")
	}
	*(dest[0].(*string)) = r.body
	return nil
}
func (t *logsTx) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	if strings.HasPrefix(sql, "SELECT logs.append_event(") {
		if t.db.checkAppend != nil {
			t.db.checkAppend(sql, args)
		}
		id, body := args[0].(string), args[5].(string)
		if prior, ok := t.pending[id]; ok {
			if prior != body {
				return appendRow{-1}
			}
			return appendRow{1}
		}
		t.pending[id] = body
		return appendRow{int64(len(t.pending))}
	}
	if sql != "SELECT payload::text FROM logs.events WHERE event_id=$1" {
		return logRow{}
	}
	return logRow{t.pending[args[0].(string)]}
}

func TestRepositoryPreservesNanosecondPayloadOnReplay(t *testing.T) {
	for _, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		for _, ns := range []int{0, 123456000, 123456499, 123456500, 123456501, 123456789, 999999999} {
			t.Run(fmt.Sprintf("%s/%d", source, ns), func(t *testing.T) {
				e := event()
				e.OccurredAt = time.Date(2026, 10, 13, 23, 59, 59, ns, time.FixedZone("UTC+8", 8*60*60))
				if source == storage.UpstreamLog {
					e.Kind = events.Call
					e.Identity.Purpose, e.Identity.TestRunID = events.TestPurpose, "test-run"
				}
				s := sanitizer(t)
				safe, err := s.Sanitize(e, observability.Details{})
				if err != nil {
					t.Fatal(err)
				}
				original := string(safe.Bytes())
				// Simulate recovering an immutable pre-fix journal event.
				safe, err = s.Restore(safe.Bytes())
				if err != nil || string(safe.Bytes()) != original {
					t.Fatal("recovery changed the event", err)
				}
				calls := 0
				db := &logsDB{lostReply: true, checkAppend: func(sql string, args []any) {
					calls++
					if !strings.Contains(sql, "$5::text::timestamptz") {
						t.Fatal("timestamp must use PostgreSQL text parsing, as the JSON timestamp does")
					}
					var body struct {
						Event struct {
							At string `json:"occurred_at"`
						} `json:"event"`
					}
					if err := json.Unmarshal([]byte(args[5].(string)), &body); err != nil {
						t.Fatal(err)
					}
					for _, format := range []int16{pgtype.TextFormatCode, pgtype.BinaryFormatCode} {
						wire, err := pgtype.NewMap().Encode(pgtype.TextOID, format, args[4], nil)
						if err != nil || string(wire) != body.Event.At {
							t.Fatal("timestamp parameter differs from immutable JSON timestamp", err)
						}
					}
					if args[5].(string) != original || !safe.Event().OccurredAt.Equal(e.OccurredAt) {
						t.Fatal("event payload or precision changed")
					}
				}}
				r := observability.Repository{Registry: logRegistry(t, true), Database: db, Source: source}
				if err := r.StoreBatch(context.Background(), []observability.SafeEvent{safe}); err != observability.ErrDelivery {
					t.Fatal("expected lost commit acknowledgement", err)
				}
				if err := r.StoreBatch(context.Background(), []observability.SafeEvent{safe}); err != nil {
					t.Fatal("replay failed", err)
				}
				if calls != 2 || len(db.rows) != 1 || db.rows[safe.ID()] != original {
					t.Fatal("replay duplicated or changed the event")
				}
			})
		}
	}
}
func (t *logsTx) Commit(context.Context) error {
	t.db.rows = t.pending
	if t.db.lostReply {
		t.db.lostReply = false
		return errors.New("reply lost after commit")
	}
	return nil
}
func (t *logsTx) Rollback(context.Context) error { return nil }
func logRegistry(t *testing.T, dual ...bool) storage.Registry {
	t.Helper()
	values := map[string]string{"DATABASE_M0_URL": "postgres://x@m:5432/b0?sslmode=disable", "DATABASE_M1_URL": "postgres://x@m:5432/b1?sslmode=disable", "DATABASE_S0_URL": "postgres://x@s:5432/b0?sslmode=disable", "DATABASE_S1_URL": "postgres://x@s:5432/b1?sslmode=disable", "DATABASE_CONTROL_URL": "postgres://x@m:5432/control?sslmode=disable", "DATABASE_LOG_URL": "postgres://x@logs:5432/logs?sslmode=disable"}
	values["LOG_STORAGE_MODE"] = "single_legacy"
	if len(dual) == 1 && dual[0] {
		values["LOG_STORAGE_MODE"] = "dual"
		values["DATABASE_UPSTREAM_LOG_URL"] = "postgres://u@upstream:5432/upstream?sslmode=disable"
	}
	r, err := storage.LoadRegistry(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestT05_09RealRepositoryCommitReplyLossUsesIndependentPGAndOneEvent(t *testing.T) {
	j := newJournal()
	b := buffer(t, j)
	safe := appendEvent(t, b, 1)
	db := &logsDB{lostReply: true}
	r := observability.Repository{Registry: logRegistry(t), Database: db}
	if _, err := b.Deliver(context.Background(), r); err != observability.ErrDelivery || len(j.files) != 1 || len(db.rows) != 1 {
		t.Fatal("unknown commit outcome reclaimed event")
	}
	if n, err := buffer(t, j).Deliver(context.Background(), r); err != nil || n != 1 || len(db.rows) != 1 || len(j.files) != 0 {
		t.Fatal("replayed event was not deduplicated")
	}
	for _, dsn := range db.connections {
		if !strings.Contains(dsn, "@logs:5432/logs") {
			t.Fatal("logs used replaceable or control database")
		}
	}
	// The repository also rejects a reused key carrying different immutable facts.
	for id := range db.rows {
		db.rows[id] = `{"different":true}`
	}
	if err := r.StoreBatch(context.Background(), []observability.SafeEvent{safe}); err != observability.ErrConflict {
		t.Fatal("event key conflict ignored")
	}
}
