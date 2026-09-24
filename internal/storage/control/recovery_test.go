package control_test

import (
	"context"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type recoveryDB struct {
	*taskDB
	event      *control.Publication
	confirmed  *control.Document
	unfinished int64
}
type recoveryTx struct {
	storage.Transaction
	db *recoveryDB
}

func (d *recoveryDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.taskDB.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return &recoveryTx{Transaction: tx, db: d}, nil
}
func (x *recoveryTx) QueryRow(ctx context.Context, sql string, args ...any) storage.Row {
	switch {
	case strings.HasPrefix(sql, "SELECT min(id),count(*)"):
		var id any
		count := x.db.unfinished
		for _, row := range x.db.records {
			if row[3] != "failed" && row[3] != "success_published" && row[3] != "success_unchanged" {
				id = row[0]
				count++
			}
		}
		return result{raw: []any{id, count}}
	case strings.HasPrefix(sql, "SELECT (SELECT id FROM control.publication_outbox"):
		var id any
		if x.db.event != nil {
			id = x.db.event.ID
		}
		return result{raw: []any{id}}
	case strings.HasPrefix(sql, "SELECT (SELECT target::text"):
		var raw any
		if x.db.confirmed != nil {
			raw = string(*x.db.confirmed)
		}
		return result{raw: []any{raw}}
	case strings.Contains(sql, "FROM control.publication_outbox WHERE"):
		return result{raw: entityColumns(*x.db.event)}
	default:
		return x.Transaction.QueryRow(ctx, sql, args...)
	}
}

func TestT14_05PersistentRecoverySnapshotPreservesUnfinishedTaskAndOwner(t *testing.T) {
	r, d, req := taskRepository(t)
	out, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	rd := &recoveryDB{taskDB: d}
	r.Database = rd
	state, err := r.RecoveryState(t.Context())
	if err != nil || state.Task.ID != req.ID || state.Owner != out.Owner || state.Task.FinishedAt != nil {
		t.Fatal(state, err)
	}
	// A terminal task with unreleased ownership still needs executor cleanup.
	d.records[req.IdempotencyKey][3] = "failed"
	state, err = r.RecoveryState(t.Context())
	if err != nil || state.Task.Status != "failed" || state.Task.FinishedAt != nil {
		t.Fatal(state, err)
	}
	if err = r.Release(t.Context(), out.Owner); err != nil {
		t.Fatal(err)
	}
	state, err = r.RecoveryState(t.Context())
	if err != nil || state.Task != nil || state.Owner.TaskID != "" {
		t.Fatal(state, err)
	}
}

func TestT14_09InconsistentControlOrFailedCommitDoesNotAuthorizeRecovery(t *testing.T) {
	for _, failure := range []string{"multiple", "owner", "orphan_event", "commit"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			if _, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil); err != nil {
				t.Fatal(err)
			}
			rd := &recoveryDB{taskDB: d}
			r.Database = rd
			switch failure {
			case "multiple":
				rd.unfinished = 1
			case "owner":
				d.records[req.IdempotencyKey][12] = "other"
			case "orphan_event":
				rd.event = &control.Publication{ID: "event", TaskID: "another-task", Status: "pending"}
			case "commit":
				d.failCommit = true
			}
			state, err := r.RecoveryState(t.Context())
			if err == nil || state.Task != nil || state.Pending != nil || state.Owner.TaskID != "" {
				t.Fatal(state, err)
			}
		})
	}
}
