package observability_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type logsDB struct {
	rows        map[string]string
	lostReply   bool
	connections []string
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
	if !strings.HasPrefix(sql, "INSERT INTO logs.events ") || !strings.Contains(sql, "ON CONFLICT (event_id) DO NOTHING") || len(args) != 6 {
		return 0, errors.New("unexpected SQL")
	}
	id := args[0].(string)
	if _, ok := t.pending[id]; ok {
		return 0, nil
	}
	t.pending[id] = args[5].(string)
	return 1, nil
}

type logRow struct{ body string }

func (r logRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("bad destinations")
	}
	*(dest[0].(*string)) = r.body
	return nil
}
func (t *logsTx) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	if sql != "SELECT payload::text FROM logs.events WHERE event_id=$1" {
		return logRow{}
	}
	return logRow{t.pending[args[0].(string)]}
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
func logRegistry(t *testing.T) storage.Registry {
	t.Helper()
	values := map[string]string{"DATABASE_M0_URL": "postgres://x@m:5432/b0?sslmode=disable", "DATABASE_M1_URL": "postgres://x@m:5432/b1?sslmode=disable", "DATABASE_S0_URL": "postgres://x@s:5432/b0?sslmode=disable", "DATABASE_S1_URL": "postgres://x@s:5432/b1?sslmode=disable", "DATABASE_CONTROL_URL": "postgres://x@m:5432/control?sslmode=disable", "DATABASE_LOG_URL": "postgres://x@logs:5432/logs?sslmode=disable"}
	values["LOG_STORAGE_MODE"] = "single_legacy"
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
