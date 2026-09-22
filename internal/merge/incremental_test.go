package merge_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
)

func TestT04_09And10IncrementalReplacesWholeRowsAndRetainsUnreturnedRows(t *testing.T) {
	current := records(t, grades, `{"ID":"03","XH":"003"}`, `{"ID":"01","XH":"001","KCH":"old","XF":3}`, `{"ID":"02","XH":"002","KCH":"old"}`)
	incoming := records(t, grades, `{"ID":"01","XF":null}`, `{"ID":"02","XH":null}`, `{"ID":"04","XH":"004"}`)
	result, err := merge.Incremental(context.Background(), grades, current, incoming)
	if err != nil {
		t.Fatal(err)
	}
	want := records(t, grades, `{"ID":"01","XF":null}`, `{"ID":"02","XH":null}`, `{"ID":"03","XH":"003"}`, `{"ID":"04","XH":"004"}`)
	assertRows(t, grades, result.Rows, want)
	for i, row := range result.Rows {
		if row.Key != want[i].Key {
			t.Fatal("target not sorted")
		}
	}
	changes := []merge.Change{
		{Key: "01", Kind: merge.Modified, BeforeRows: 1, AfterRows: 1},
		{Key: "02", Kind: merge.Modified, BeforeRows: 1, AfterRows: 1},
		{Key: "04", Kind: merge.Added, AfterRows: 1},
	}
	if result.DeletionsKnown || !reflect.DeepEqual(result.Changes.Changes, changes) {
		t.Fatalf("incremental inferred deletions or incorrect changes: %+v", result)
	}
	// Existing rows and updated rows both have detached source token storage.
	current[0].Fields["XH"].Raw[1] = 'X'
	incoming[2].Fields["XH"].Raw[1] = 'X'
	assertRows(t, grades, result.Rows, want)
}

func TestT04_11RepeatedAndEmptyWindowsAreIdempotent(t *testing.T) {
	current := records(t, grades, `{"ID":"01","XF":3}`, `{"ID":"02","XF":4}`)
	incoming := records(t, grades, `{"ID":"01","XF":3.0}`, `{"ID":"03"}`)
	first, err := merge.Incremental(context.Background(), grades, current, incoming)
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range [][]ingest.Record{incoming, nil, incoming, nil} {
		next, err := merge.Incremental(context.Background(), grades, first.Rows, batch)
		if err != nil || len(next.Changes.Changes) != 0 || next.DeletionsKnown {
			t.Fatalf("replayed/empty window changed state: %+v %v", next, err)
		}
		assertRows(t, grades, next.Rows, records(t, grades, `{"ID":"01","XF":3.0}`, `{"ID":"02","XF":4}`, `{"ID":"03"}`))
		first = next
	}
}

func TestT04_17And20IncrementalHistoryKeepsLatestEntireSourceRecord(t *testing.T) {
	for _, d := range historyTables {
		latest := historyRow(d.key, d.field, `null`, `"2026-09-01 02:00:00"`)
		older := historyRow(d.key, d.field, `"old"`, `"2026-09-01 01:00:00"`)
		got, err := merge.Incremental(context.Background(), d.table, records(t, d.table, latest), records(t, d.table, older, older))
		if err != nil || len(got.Changes.Changes) != 0 {
			t.Fatalf("late old row overwrote latest: %v", err)
		}
		assertRows(t, d.table, got.Rows, records(t, d.table, latest))
		got, err = merge.Incremental(context.Background(), d.table, records(t, d.table, older), records(t, d.table, latest))
		if err != nil {
			t.Fatal(err)
		}
		assertRows(t, d.table, got.Rows, records(t, d.table, latest))
		conflict := historyRow(d.key, d.field, `"conflicting"`, `"2026-09-01 02:00:00"`)
		if result, err := merge.Incremental(context.Background(), d.table, got.Rows, records(t, d.table, conflict)); !errors.Is(err, merge.ErrHistoryConflict) || result.Rows != nil {
			t.Fatal("cross-round conflict produced target")
		}
	}
}

func TestIncrementalRejectsDuplicateInputsAndInvalidBaselineWithoutMutation(t *testing.T) {
	row := records(t, grades, `{"ID":"01","XF":3}`)
	for _, input := range []struct{ base, incoming []ingest.Record }{
		{row, append(row, row...)}, {append(row, row...), nil},
	} {
		got, err := merge.Incremental(context.Background(), grades, input.base, input.incoming)
		if !errors.Is(err, merge.ErrDuplicate) || got.Rows != nil {
			t.Fatal("duplicate key produced target")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := merge.Incremental(ctx, grades, row, nil); !errors.Is(err, context.Canceled) || got.Rows != nil {
		t.Fatal("cancelled target accepted")
	}
	assertRows(t, grades, row, records(t, grades, `{"ID":"01","XF":3}`))
}
