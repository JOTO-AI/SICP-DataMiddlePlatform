package read_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func keyBudget(t *testing.T, limits read.Limits) *read.Budget {
	t.Helper()
	b, err := read.NewBudget(limits, &scanClock{now: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestT17_07BatchAssociationsUseFixedHandleAndSharedBudget(t *testing.T) {
	keys := make([]string, 1001)
	for i := range keys {
		keys[i] = fmt.Sprintf("%04d", 1001-i)
	}
	keys = append(keys, "0001")
	originalKeys := append([]string(nil), keys...)
	calls := 0
	reader := matcherFunc(func(ctx context.Context, h read.Handle, table, after string, n int, filters []source.Filter) ([]source.SourceRow, error) {
		calls++
		if h != original || table != "dwd_xqjbsjzlb" || len(filters) != 1 || filters[0].Column != "XQH" || len(filters[0].Values) > 1000 {
			t.Fatal(h, table, filters)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("SQL lost shared deadline")
		}
		set := filters[0].Values
		if !sort.StringsAreSorted(set) {
			t.Fatal("keys not stable")
		}
		rows := []source.SourceRow{}
		for _, key := range set {
			if key > after {
				raw, _ := json.Marshal(key)
				rows = append(rows, source.SourceRow{"XQH": raw})
				if len(rows) == n {
					break
				}
			}
		}
		return rows, nil
	})
	b := keyBudget(t, read.DefaultLimits())
	index, err := read.LookupMany(t.Context(), reader, original, "dwd_xqjbsjzlb", "XQH", keys, b)
	if err != nil || len(index) != 1001 || calls != 4 || !reflect.DeepEqual(keys, originalKeys) {
		t.Fatal(len(index), calls, err)
	}
	if string(index["0001"]["XQH"]) != `"0001"` {
		t.Fatal("leading zero lost")
	}
	// An empty association set must not query an entire table.
	before := calls
	rows, err := read.CollectKeys(t.Context(), reader, original, "dwd_xqjbsjzlb", "XQH", nil, b)
	if err != nil || len(rows) != 0 || calls != before {
		t.Fatal(rows, err, calls)
	}
}

func TestT17_09BatchAssociationFailuresNeverReturnPartialRows(t *testing.T) {
	for _, kind := range []string{"rows", "bytes", "keys", "batches", "cancel", "failure", "duplicate", "outside-set", "duplicate-association"} {
		t.Run(kind, func(t *testing.T) {
			limits := read.DefaultLimits()
			limits.BatchSize = 1
			keys := []string{"001", "002"}
			if kind == "rows" {
				limits.Rows = 1
			}
			if kind == "bytes" {
				limits.Bytes = 140
			}
			if kind == "keys" {
				limits.Bytes = 1
			}
			if kind == "batches" {
				limits.Batches = 1
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "cancel" {
				cancel()
			}
			calls := 0
			reader := matcherFunc(func(_ context.Context, _ read.Handle, table, after string, n int, _ []source.Filter) ([]source.SourceRow, error) {
				calls++
				if calls == 2 && kind == "failure" {
					return nil, errors.New("SQL failed")
				}
				if calls > 2 {
					return nil, nil
				}
				id := fmt.Sprintf("%03d", calls)
				if kind == "duplicate" && calls == 2 {
					id = "001"
				}
				if kind == "outside-set" {
					id = "003"
				}
				raw, _ := json.Marshal(id)
				row := source.SourceRow{"XQH": raw}
				if kind == "duplicate-association" {
					row["XQMC"] = json.RawMessage(`"same"`)
				}
				return []source.SourceRow{row}, nil
			})
			column := "XQH"
			if kind == "duplicate-association" {
				column = "XQMC"
				keys = []string{"same"}
			}
			result, err := read.LookupMany(ctx, reader, original, "dwd_xqjbsjzlb", column, keys, keyBudget(t, limits))
			if err == nil || result != nil {
				t.Fatal("partial result", result, err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if kind == "duplicate-association" && !errors.Is(err, read.ErrAmbiguous) {
				t.Fatal(err)
			}
			if (kind == "keys" || kind == "cancel") && calls != 0 {
				t.Fatal("read after exhausted budget", calls)
			}
		})
	}
	budget := keyBudget(t, read.DefaultLimits())
	if !errors.Is(budget.Reserve(-1), read.ErrBudget) {
		t.Fatal("negative memory debit accepted")
	}
	limits := read.DefaultLimits()
	limits.Rows = 1
	shared := keyBudget(t, limits)
	reader := matcherFunc(func(_ context.Context, _ read.Handle, _, _ string, _ int, _ []source.Filter) ([]source.SourceRow, error) {
		return []source.SourceRow{{"XQH": json.RawMessage(`"001"`)}}, nil
	})
	if _, err := read.LookupMany(t.Context(), reader, original, "dwd_xqjbsjzlb", "XQH", []string{"001"}, shared); err != nil {
		t.Fatal(err)
	}
	if result, err := read.LookupMany(t.Context(), reader, original, "dwd_xqjbsjzlb", "XQH", []string{"001"}, shared); !errors.Is(err, read.ErrBudget) || result != nil {
		t.Fatal("second association reset shared row budget", err)
	}
}
