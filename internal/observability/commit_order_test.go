package observability_test

import (
	"context"
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"testing"
)

type rollbackLogDB struct {
	inner logsDB
	tx    *rollbackLogTx
}
type rollbackLogTx struct {
	*logsTx
	rolled bool
}

func (d *rollbackLogDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	v, e := d.inner.Begin(ctx, c)
	d.tx = &rollbackLogTx{logsTx: v.(*logsTx)}
	return d.tx, e
}
func (t *rollbackLogTx) Commit(context.Context) error   { return errors.New("commit failed") }
func (t *rollbackLogTx) Rollback(context.Context) error { t.rolled = true; return nil }
func TestT29_02FailedCommitAndCancellationNeverAcknowledge(t *testing.T) {
	b := buffer(t, newJournal())
	safe := appendEvent(t, b, 1)
	db := &rollbackLogDB{}
	repo := observability.Repository{Registry: logRegistry(t), Database: db}
	if e := repo.StoreBatch(t.Context(), []observability.SafeEvent{safe}); e != observability.ErrDelivery || len(db.inner.rows) != 0 || !db.tx.rolled {
		t.Fatal("uncommitted acknowledgement", e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if e := repo.StoreBatch(ctx, []observability.SafeEvent{safe}); e != observability.ErrDelivery || len(db.inner.rows) != 0 {
		t.Fatal(e)
	}
}
func TestT29_03ReplayConflictAndUnknownCommitKeepOriginal(t *testing.T) {
	b := buffer(t, newJournal())
	safe := appendEvent(t, b, 1)
	db := &logsDB{lostReply: true}
	repo := observability.Repository{Registry: logRegistry(t), Database: db}
	if e := repo.StoreBatch(t.Context(), []observability.SafeEvent{safe}); e != observability.ErrDelivery {
		t.Fatal(e)
	}
	if e := repo.StoreBatch(t.Context(), []observability.SafeEvent{safe}); e != nil || len(db.rows) != 1 {
		t.Fatal(e)
	}
	db.rows[safe.ID()] = `{"different":true}`
	if e := repo.StoreBatch(t.Context(), []observability.SafeEvent{safe}); e != observability.ErrConflict || db.rows[safe.ID()] != `{"different":true}` {
		t.Fatal("conflict overwrote original")
	}
}
