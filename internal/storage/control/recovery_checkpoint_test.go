package control_test

import (
	"context"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type checkpointDB struct {
	*taskDB
	document string
}
type checkpointTx struct {
	storage.Transaction
	db       *checkpointDB
	document string
}

func (d *checkpointDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.taskDB.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return &checkpointTx{Transaction: tx, db: d, document: d.document}, nil
}
func (x *checkpointTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if strings.HasPrefix(sql, "INSERT INTO control.checkpoints") {
		x.document = args[1].(string)
		return 1, nil
	}
	return x.Transaction.Exec(ctx, sql, args...)
}
func (x *checkpointTx) QueryRow(ctx context.Context, sql string, args ...any) storage.Row {
	if strings.HasPrefix(sql, "SELECT payload::text FROM control.checkpoints") {
		return result{raw: []any{x.document}}
	}
	return x.Transaction.QueryRow(ctx, sql, args...)
}
func (x *checkpointTx) Commit(ctx context.Context) error {
	if err := x.Transaction.Commit(ctx); err != nil {
		return err
	}
	x.db.document = x.document
	return nil
}

func TestT14_07CheckpointPersistsOnlyUnderCurrentOwnership(t *testing.T) {
	for _, failure := range []string{"none", "owner", "commit", "terminal"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			launch, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			db := &checkpointDB{taskDB: d, document: `{"old":true}`}
			r.Database = db
			switch failure {
			case "owner":
				launch.Owner.Generation++
			case "commit":
				d.failCommit = true
			case "terminal":
				d.records[req.IdempotencyKey][3] = "failed"
			}
			doc := control.Document(`{"next":true}`)
			out, err := r.WithOwnership(launch.Owner).RecoveryCheckpoint(t.Context(), &doc, req.AcceptedAt)
			if failure == "none" {
				if err != nil || out != doc || db.document != string(doc) {
					t.Fatal(out, err)
				}
			} else if err == nil || out != "" || db.document != `{"old":true}` {
				t.Fatal("checkpoint escaped fence or rollback", out, err)
			}
		})
	}
}
