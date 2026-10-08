package source_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type copyDatabase struct {
	*database
	wrong, fail bool
	columns     []string
	rows        [][]any
}
type copyTransaction struct {
	*transaction
	owner *copyDatabase
}

func (d *copyDatabase) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.database.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return copyTransaction{tx.(*transaction), d}, nil
}
func (t copyTransaction) CopyFrom(_ context.Context, table string, columns []string, rows [][]any) (int64, error) {
	if table != "dwd_xqjbsjzlb" {
		return 0, errors.New("wrong table")
	}
	t.owner.columns = append([]string(nil), columns...)
	t.owner.rows = append(t.owner.rows, rows...)
	if t.owner.fail {
		return 0, errors.New("copy failed")
	}
	if t.owner.wrong {
		return 0, nil
	}
	return int64(len(rows)), nil
}
func TestBT05_CopyBindingAndFailuresNeverFallback(t *testing.T) {
	for _, mode := range []string{"success", "count", "copy", "commit", "missing-adapter"} {
		r, db, _, target := setup(t)
		d := &copyDatabase{database: db, wrong: mode == "count", fail: mode == "copy"}
		db.failCommit = mode == "commit"
		r.UseCopy = true
		if mode != "missing-adapter" {
			r.Database = d
		}
		report, err := r.Rebuild(t.Context(), target, "dwd_xqjbsjzlb", &iterator{rows: []source.SourceRow{campus("001")}}, 500)
		if (err == nil) != (mode == "success") || report.Complete != (mode == "success") {
			t.Fatal(mode, report, err)
		}
		for _, call := range db.calls {
			if strings.HasPrefix(call.sql, "INSERT") {
				t.Fatal("silent fallback")
			}
		}
		if mode == "success" && (!reflect.DeepEqual(d.columns, []string{"XQH", "XQMC", "XQCX", "ETL_TIME"}) || !reflect.DeepEqual(d.rows, [][]any{{"001", "campus", nil, nil}})) {
			t.Fatal("COPY binding differs", d.columns, d.rows)
		}
		if mode != "success" && !db.transactions[0].rolledBack {
			t.Fatal("failed copy not rolled back")
		}
	}
}
