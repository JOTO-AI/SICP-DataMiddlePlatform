package source_test

import (
	"context"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type indexDatabase struct {
	*database
	valid bool
}

type missingClassIndexDB struct {
	*database
	missing string
}
type missingClassIndexTx struct {
	*transaction
	missing string
}

func (d missingClassIndexDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.database.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return missingClassIndexTx{transaction: tx.(*transaction), missing: d.missing}, nil
}
func (tx missingClassIndexTx) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	tx.db.calls = append(tx.db.calls, call{sql, args})
	return indexResult(args[1] != tx.missing)
}

func TestT24_06ClassIndexesAreRequiredByCandidateStorageChecks(t *testing.T) {
	for _, table := range []string{"dwd_bzkspksjzlb", "dwd_bzkskbsjzlb"} {
		for _, missing := range []bool{false, true} {
			r, d, _, target := setup(t)
			name := ""
			if missing {
				name = "source." + table + "_class_cursor_c"
			}
			r.Database = missingClassIndexDB{database: d, missing: name}
			err := r.CheckPrimaryIndex(t.Context(), target.Position, table)
			if (err != nil) != missing {
				t.Fatal(table, missing, err)
			}
			if len(d.calls) != 1+len(source.ReadIndexes(table)) {
				t.Fatal(d.calls)
			}
			last := d.calls[len(d.calls)-1]
			if last.args[1] != "source."+table+"_class_cursor_c" || !strings.Contains(last.sql, "indisvalid") || !strings.Contains(last.sql, "indisready") || !strings.Contains(last.sql, "indislive") {
				t.Fatal(last)
			}
		}
	}
}

type indexTransaction struct {
	*transaction
	valid bool
}
type indexResult bool

func (r indexResult) Scan(dest ...any) error { *dest[0].(*bool) = bool(r); return nil }
func (d indexDatabase) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.database.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return indexTransaction{transaction: tx.(*transaction), valid: d.valid}, nil
}
func (t indexTransaction) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	t.db.calls = append(t.db.calls, call{sql, args})
	return indexResult(t.valid)
}

func TestT12_04PrimaryIndexValidationUsesFixedTableAndAcknowledgedRead(t *testing.T) {
	for _, kind := range []string{"valid", "invalid", "commit"} {
		t.Run(kind, func(t *testing.T) {
			r, d, _, target := setup(t)
			d.failCommit = kind == "commit"
			r.Database = indexDatabase{database: d, valid: kind != "invalid"}
			err := r.CheckPrimaryIndex(t.Context(), target.Position, "dwd_xqjbsjzlb")
			if (kind == "valid") != (err == nil) {
				t.Fatal(kind, err)
			}
			wantCalls := 2
			if kind == "invalid" {
				wantCalls = 1
			}
			if len(d.calls) != wantCalls || d.calls[0].args[0] != "source.dwd_xqjbsjzlb" || d.calls[0].args[1] != "XQH" {
				t.Fatal(d.calls)
			}
			for _, required := range []string{"indisprimary", "indisvalid", "indisready", "indnkeyatts=1", "attnotnull", "has_table_privilege"} {
				if !strings.Contains(d.calls[0].sql, required) {
					t.Fatal(d.calls[0].sql)
				}
			}
			before := len(d.calls)
			if r.CheckPrimaryIndex(t.Context(), target.Position, "arbitrary") == nil || len(d.calls) != before {
				t.Fatal("unlisted identifier used")
			}
		})
	}
}
