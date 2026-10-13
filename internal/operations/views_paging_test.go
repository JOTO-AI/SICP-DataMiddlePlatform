package operations

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type viewRows struct {
	items  [][]any
	pos    int
	err    error
	closed bool
}

func (r *viewRows) Next() bool {
	if r.pos >= len(r.items) {
		return false
	}
	r.pos++
	return true
}
func (r *viewRows) Scan(d ...any) error {
	row := r.items[r.pos-1]
	if len(d) != len(row) {
		return errors.New("column mismatch")
	}
	for i, v := range row {
		if v != nil {
			reflect.ValueOf(d[i]).Elem().Set(reflect.ValueOf(v))
		}
	}
	return nil
}
func (r *viewRows) Err() error { return r.err }
func (r *viewRows) Close()     { r.closed = true }

type viewTx struct {
	fakeTx
	query func(string, []any) (storage.Rows, error)
}

func (v *viewTx) Query(_ context.Context, q string, a ...any) (storage.Rows, error) {
	return v.query(q, a)
}

type viewDB struct{ tx storage.Transaction }

func (v viewDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	return v.tx, nil
}

func TestT31_01And03EventPagingPreservesRecoveryHistoryAndUnknownTime(t *testing.T) {
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	previous := "original-task"
	rows := &viewRows{items: [][]any{{"rerun-task", "force_full", "running", &previous, at, nil, nil, nil, nil, nil}, {"original-task", "force_full", "failed", nil, at.Add(-time.Hour), nil, nil, nil, nil, nil}}}
	tx := &viewTx{query: func(q string, a []any) (storage.Rows, error) {
		if !strings.Contains(q, "(accepted_at,id)<($1,$2)") || a[1] != "cursor-id" || a[2] != 2 {
			t.Fatal("unstable or unbounded paging")
		}
		return rows, nil
	}}
	got, err := (Repository{Database: viewDB{tx}}).Events(t.Context(), at, "cursor-id", 2)
	if err != nil || len(got) != 2 || got[0].TaskID != "rerun-task" || got[0].Predecessor == nil || *got[0].Predecessor != "original-task" || got[0].FinishedAt != nil || got[1].PublishedAt != nil || !rows.closed {
		t.Fatal("invented/replaced event history", got, err)
	}
}

func TestT31_02And03OperationFactsDoNotInferPublication(t *testing.T) {
	instance := "S"
	slot := int16(1)
	generation := int64(7)
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	rows := &viewRows{items: [][]any{{"copy-1", "ready", &instance, &slot, &generation, nil, &at, "builds"}}}
	tx := &viewTx{fakeTx: fakeTx{row: func(string, []any) storage.Row {
		return fakeRow(func(d ...any) error { *d[0].(*bool) = true; return nil })
	}}, query: func(q string, a []any) (storage.Rows, error) {
		if !strings.Contains(q, "control.candidates") || a[0] != "task" || a[1] != "copy-0" || a[2] != 1 {
			t.Fatal("wrong operation page")
		}
		return rows, nil
	}}
	got, err := (Repository{Database: viewDB{tx}}).Operations(t.Context(), "task", "builds", "copy-0", 1)
	if err != nil || len(got) != 1 || got[0].State != "ready" || got[0].StartedAt != nil || got[0].Parent == nil || *got[0].Parent != "builds" || got[0].Kind == "publication" || !rows.closed {
		t.Fatal(got, err)
	}
	rows.pos = 0
	rows.err = errors.New("cursor failed after first row")
	tx.committed = false
	if got, err = (Repository{Database: viewDB{tx}}).Operations(t.Context(), "task", "builds", "copy-0", 1); err != ErrUnavailable || got != nil || tx.committed {
		t.Fatal("partial page passed as success")
	}
}

func TestT31_10OverviewAdmissionReasonsAndFailure(t *testing.T) {
	for _, tc := range []struct {
		initialized, busy, broken bool
		want                      string
	}{{false, false, false, "initialization_required"}, {true, false, false, "available_recheck_on_submit"}, {true, true, false, "task_or_publication_busy"}, {true, false, true, ""}} {
		tx := &fakeTx{row: func(string, []any) storage.Row {
			return fakeRow(func(d ...any) error {
				if tc.broken {
					return errors.New("control unavailable")
				}
				*d[0].(*bool), *d[1].(*bool) = tc.initialized, tc.busy
				return nil
			})
		}}
		got, err := fixtureRepo(tx).ActionReasons(t.Context())
		if tc.broken {
			if err != ErrUnavailable || got != nil {
				t.Fatal("dependency failure reported ready")
			}
		} else if err != nil || got["force_full"] != tc.want {
			t.Fatal(got, err)
		}
	}
}
