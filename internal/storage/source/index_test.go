package source_test

import (
	"context"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type indexDatabase struct {
	*database
	valid bool
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
			if len(d.calls) != 1 || d.calls[0].args[0] != "source.dwd_xqjbsjzlb" || d.calls[0].args[1] != "XQH" {
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
