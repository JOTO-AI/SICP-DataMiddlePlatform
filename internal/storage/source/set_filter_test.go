package source_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestT17_07BoundedSetFiltersBindAllValues(t *testing.T) {
	r, db, _, target := setup(t)
	keys := []string{"001", `x' OR TRUE --`, `100%_\`}
	_, err := r.ReadMatching(t.Context(), target.Position, "dwd_xqjbsjzlb", "", 10, []source.Filter{{Column: "XQH", Values: keys}, {Column: "XQMC", Value: "100%_", Contains: true}})
	if err != nil {
		t.Fatal(err)
	}
	call := db.calls[0]
	if !strings.Contains(call.sql, `"XQH" COLLATE "C" = ANY($3::text[])`) || !strings.Contains(call.sql, `strpos("XQMC", $4) > 0`) || strings.Contains(call.sql, "OR TRUE") || !reflect.DeepEqual(call.args, []any{"", 10, keys, "100%_"}) {
		t.Fatal(call)
	}
	keys[0] = "changed"
	if call.args[2].([]string)[0] != "001" {
		t.Fatal("bound key slice aliases caller")
	}
	for _, filter := range []source.Filter{
		{Column: "XQH", Values: []string{}}, {Column: "XQH", Values: make([]string, 1001)}, {Column: "XQH", Values: []string{"001"}, Value: "002"}, {Column: "XQH", Values: []string{"001"}, Contains: true}, {Column: `XQH" OR TRUE --`, Values: []string{"001"}}, {Column: "ETL_TIME", Values: []string{"001"}}, {Column: "XQH", Values: []string{strings.Repeat("x", 4097)}}, {Column: "XQH", Values: []string{"x\x00"}},
	} {
		r, db, _, target := setup(t)
		if _, err := r.ReadMatching(t.Context(), target.Position, "dwd_xqjbsjzlb", "", 10, []source.Filter{filter}); !errors.Is(err, source.ErrInput) || len(db.transactions) != 0 {
			t.Fatal("invalid set acquired database", err)
		}
	}
	r, db, _, target = setup(t)
	db.readRows = [][]any{{"001", "campus", nil, nil}}
	db.rowError = errors.New("later read failed")
	if rows, err := r.ReadMatching(t.Context(), target.Position, "dwd_xqjbsjzlb", "", 10, []source.Filter{{Column: "XQH", Values: []string{"001"}}}); err == nil || rows != nil || !db.closed {
		t.Fatal("partial set query success", rows, err)
	}
}
