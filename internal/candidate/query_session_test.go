package candidate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type memoMatcher struct {
	calls int
	empty bool
}

func (r *memoMatcher) ReadMatching(context.Context, read.Handle, string, string, int, []source.Filter) ([]source.SourceRow, error) {
	r.calls++
	if r.empty {
		return nil, nil
	}
	return []source.SourceRow{{"KCH": json.RawMessage(`"001"`)}}, nil
}
func TestBT07_MemoScopeBoundsEmptyAnswersAndDetachedRows(t *testing.T) {
	for _, empty := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		sourceRows := &memoMatcher{empty: empty}
		h := read.Handle{Position: storage.Position{Instance: storage.Main}, Generation: 1, Combination: "C1"}
		m := &queryMemo{ctx: ctx, source: sourceRows, handle: h, versions: map[string]string{"courses": "v1"}, rows: map[string][]source.SourceRow{}, limit: 1024}
		filters := []source.Filter{{Column: "KCH", Value: "001"}}
		first, err := m.ReadMatching(ctx, h, "courses", "", 2, filters)
		if err != nil {
			t.Fatal(err)
		}
		if !empty {
			first[0]["KCH"] = json.RawMessage(`"changed"`)
		}
		again, err := m.ReadMatching(ctx, h, "courses", "", 2, filters)
		if err != nil || sourceRows.calls != 1 || !empty && string(again[0]["KCH"]) != `"001"` {
			t.Fatal("cache alias or missing hit", err)
		}
		other := h
		other.Generation++
		if _, err = m.ReadMatching(ctx, other, "courses", "", 2, filters); !errors.Is(err, read.ErrHandle) {
			t.Fatal("cross-generation hit")
		}
		m.limit = m.used // A full cache must fall back without dropping the lookup.
		filters[0].Value = "002"
		for range 2 {
			if _, err = m.ReadMatching(ctx, h, "courses", "", 2, filters); err != nil {
				t.Fatal(err)
			}
		}
		if sourceRows.calls != 3 {
			t.Fatal("full cache fabricated result")
		}
		cancel()
		if _, err = m.ReadMatching(t.Context(), h, "courses", "", 2, filters); !errors.Is(err, context.Canceled) {
			t.Fatal("expired session reused")
		}
	}
}
