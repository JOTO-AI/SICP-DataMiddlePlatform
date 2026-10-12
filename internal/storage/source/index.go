package source

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type ReadIndex struct {
	Name    string
	Columns []string
}

// ReadIndexes describes allowlisted btree keys, all ascending COLLATE C.
func ReadIndexes(name string) []ReadIndex {
	table, ok := tables[name]
	if !ok {
		return nil
	}
	out := []ReadIndex{{Name: name + "_cursor_c", Columns: []string{table.PrimaryKey}}}
	switch name {
	case "dwd_bzkscjzlb", "dwd_bzkskbsjzlb":
		out = append(out, ReadIndex{Name: name + "_student_c", Columns: []string{"XH", "ID"}})
	case "dwd_bzkspksjzlb":
		out = append(out, ReadIndex{Name: name + "_class_course_c", Columns: []string{"JXBH", "JHKCH", "PKH"}}, ReadIndex{Name: name + "_term_c", Columns: []string{"KKXND", "KKXQM", "PKH"}})
	}
	if name == "dwd_bzkspksjzlb" || name == "dwd_bzkskbsjzlb" {
		out = append(out, ReadIndex{Name: name + "_class_cursor_c", Columns: []string{"JXBH", table.PrimaryKey}})
	}
	return out
}

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
	for _, index := range ReadIndexes(name) {
		err = tx.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_am am ON am.oid=c.relam
WHERE i.indrelid=to_regclass($1) AND i.indexrelid=to_regclass($2)
AND i.indisvalid AND i.indisready AND i.indislive AND am.amname='btree'
AND i.indpred IS NULL AND i.indexprs IS NULL AND i.indnkeyatts=cardinality($3::text[]) AND i.indnatts=i.indnkeyatts
AND ARRAY(SELECT a.attname::text FROM unnest(i.indkey) WITH ORDINALITY AS k(attnum,n) JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum ORDER BY k.n)=$3::text[]
AND NOT EXISTS (SELECT 1 FROM unnest(i.indcollation) AS col(oid) WHERE col.oid<>'pg_catalog."C"'::regcollation)
AND NOT EXISTS (SELECT 1 FROM unnest(i.indoption) AS opt(flags) WHERE opt.flags<>0)
AND NOT EXISTS (SELECT 1 FROM unnest(i.indclass) AS cl(oid) JOIN pg_opclass op ON op.oid=cl.oid WHERE op.opcname<>'text_ops' OR op.opcnamespace<>'pg_catalog'::regnamespace))`, "source."+name, "source."+index.Name, index.Columns).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return ErrTarget
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
