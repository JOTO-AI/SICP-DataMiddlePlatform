package adapters

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type txResult struct {
	pgx.Tx
	commit, rollback error
}

func (t txResult) Commit(context.Context) error   { return t.commit }
func (t txResult) Rollback(context.Context) error { return t.rollback }
func TestT15_10SQLProofDoesNotInferEndFromLostReplyOrClosedClientTransaction(t *testing.T) {
	for _, scenario := range []string{"commit_ack", "rollback_ack", "commit_unknown", "rollback_unknown", "already_closed"} {
		t.Run(scenario, func(t *testing.T) {
			p := &Postgres{executions: map[string]execution{"sql-1": {}}}
			lost := errors.New("synthetic secret driver message")
			raw := txResult{}
			want := true
			switch scenario {
			case "commit_unknown":
				raw.commit = lost
				want = false
			case "rollback_unknown":
				raw.rollback = lost
				want = false
			case "already_closed":
				raw.rollback = pgx.ErrTxClosed
				want = false
			}
			tx := pgTransaction{tx: raw, owner: p, id: "sql-1"}
			if scenario == "commit_ack" || scenario == "commit_unknown" {
				_ = tx.Commit(t.Context())
			} else {
				_ = tx.Rollback(t.Context())
			}
			if p.executions["sql-1"].done != want {
				t.Fatal("unproven SQL termination")
			}
			if want {
				ok, e := p.Stopped(t.Context(), "sql-1")
				if e != nil || !ok {
					t.Fatal(ok, e)
				}
				p.Forget("sql-1")
				if len(p.executions) != 0 {
					t.Fatal("proof retained after durable finish")
				}
			}
		})
	}
	if !errors.Is(pgError(context.Background(), pgx.ErrNoRows), sql.ErrNoRows) {
		t.Fatal("missing row semantics lost")
	}
	if pgError(context.Background(), errors.New("synthetic secret")) != ErrPostgres {
		t.Fatal("raw error exposed")
	}
}
