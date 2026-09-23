package source

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// CheckPrimaryIndex verifies the exact fixed source table, TEXT primary key,
// usable primary index and SELECT access. The caller holds candidate validation
// ownership; this method neither opens admission nor creates derived data.
func (r Repository) CheckPrimaryIndex(ctx context.Context, p storage.Position, name string) error {
	table, ok := tables[name]
	if !ok || r.Database == nil {
		return ErrInput
	}
	connection, err := r.Registry.Business(p)
	if err != nil {
		return err
	}
	tx, err := r.Database.Begin(ctx, connection)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_index i JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=i.indkey[0] WHERE i.indrelid=to_regclass($1) AND i.indisprimary AND i.indisvalid AND i.indisready AND i.indnkeyatts=1 AND a.attname=$2 AND a.atttypid='text'::regtype AND a.attnotnull) AND has_table_privilege($1,'SELECT')`, "source."+table.Name, table.PrimaryKey).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrTarget
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
