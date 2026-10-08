//go:build integration

package integration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// Preserve the real SQL execution proof while failing a COPY after an earlier
// committed batch. The recovery must derive state from persisted evidence.
type copyFaultDatabase struct {
	*adapters.Postgres
	calls  atomic.Int64
	tamper *storage.Connection
}

type copyFaultTransaction struct {
	storage.Transaction
	owner *copyFaultDatabase
}

func (d *copyFaultDatabase) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.Postgres.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	if d.tamper != nil && c != *d.tamper {
		return tx, nil
	}
	return copyFaultTransaction{tx, d}, nil
}

func (t copyFaultTransaction) Query(ctx context.Context, sql string, args ...any) (storage.Rows, error) {
	return t.Transaction.(storage.QueryExecutor).Query(ctx, sql, args...)
}

func (t copyFaultTransaction) CopyFrom(ctx context.Context, table string, columns []string, rows [][]any) (int64, error) {
	if t.owner.tamper != nil {
		n, err := t.Transaction.(storage.CopyExecutor).CopyFrom(ctx, table, columns, rows)
		if err == nil && table == "dwd_bzkscjzlb" {
			_, err = t.Transaction.Exec(ctx, `UPDATE source.dwd_bzkscjzlb SET "KCCJ"='999' WHERE "ID"=(SELECT min("ID") FROM source.dwd_bzkscjzlb)`)
			t.owner.calls.Add(1)
		}
		return n, err
	}
	if t.owner.calls.Add(1) == 2 {
		return 0, errors.New("synthetic COPY interruption")
	}
	return t.Transaction.(storage.CopyExecutor).CopyFrom(ctx, table, columns, rows)
}

func TestI05_B10StandbySingleFieldCorruptionCannotPublish(t *testing.T) {
	f := newRuntime(t, false)
	standby, err := f.wc.Databases.Business(storage.Position{Instance: storage.Standby, Slot: 0})
	if err != nil {
		t.Fatal(err)
	}
	fault := &copyFaultDatabase{Postgres: f.wr.Database.(*adapters.Postgres), tamper: &standby}
	f.wr.Database = fault
	admitTask(t, f, "initialize", "corrupt-standby")
	if err := f.worker.Engine.Cycle(f.e.ctx); err == nil || fault.calls.Load() == 0 {
		t.Fatal("corruption not detected", err)
	}
	if countControl(t, f, "SELECT count(*) FROM control.candidates WHERE state='ready' AND instance='M'") != 1 ||
		countControl(t, f, "SELECT count(*) FROM control.candidates WHERE state='ready' AND instance='S'") != 0 ||
		countControl(t, f, "SELECT count(*) FROM control.publication_outbox") != 0 ||
		countControl(t, f, "SELECT count(*) FROM control.source_progress") != 0 {
		t.Fatal("M evidence incorrectly authorized corrupt S")
	}
	if _, exists, err := f.wr.Active.Read(f.e.ctx); err != nil || exists {
		t.Fatal("corrupt standby published", err)
	}
}

func TestI05_B10CopyFailureCannotPublishAndCanRecover(t *testing.T) {
	f := newRuntime(t, false)
	fault := &copyFaultDatabase{Postgres: f.wr.Database.(*adapters.Postgres)}
	f.wr.Database = fault
	admitTask(t, f, "initialize", "copy-recovery")
	if err := f.worker.Engine.Cycle(f.e.ctx); err == nil || fault.calls.Load() != 2 {
		t.Fatal("COPY fault was not reached", err)
	}
	if countControl(t, f, "SELECT count(*) FROM control.candidates WHERE state='ready'") != 0 ||
		countControl(t, f, "SELECT count(*) FROM control.publication_outbox") != 0 ||
		countControl(t, f, "SELECT count(*) FROM control.source_progress") != 0 {
		t.Fatal("partial import became publishable")
	}
	if _, exists, err := f.wr.Active.Read(f.e.ctx); err != nil || exists {
		t.Fatal("partial initialization exposed an active route", err)
	}
	if err := f.worker.Engine.Cycle(f.e.ctx); err != nil {
		t.Fatal("COPY recovery failed", err)
	}
	if task := loadTask(t, f, "copy-recovery"); task.Status != "success_published" {
		t.Fatal(task.Status)
	}
	if countControl(t, f, "SELECT count(*) FROM control.candidates WHERE task_id='copy-recovery' AND state='ready'") != 2 {
		t.Fatal("recovery did not independently verify both copies")
	}
	assertProgress(t, f, "copy-recovery")
}
