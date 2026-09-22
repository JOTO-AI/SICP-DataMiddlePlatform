package merge_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
)

var historyTables = []struct{ table, key, field string }{
	{"dwd_bzksxjjbsjzlb", "XH", "YXSH"},
	{"dwd_bzksgrtxsjzlb", "XH", "YDDH"},
	{"dwd_grtxfszlb", "JGH", "YDDH"},
}

func historyRow(key, field, content, timestamp string) string {
	return fmt.Sprintf(`{"%s":"001","%s":%s,"ETL_TIME":%s}`, key, field, content, timestamp)
}

func assertRows(t *testing.T, table string, got, want []ingest.Record) {
	t.Helper()
	report, err := merge.Compare(context.Background(), table, got, want)
	if err != nil || len(report.Changes) != 0 {
		t.Fatalf("rows differ: %+v %v", report, err)
	}
}

func TestT04_16And17LatestWholeRowIsOrderIndependentAcrossRounds(t *testing.T) {
	for _, d := range historyTables {
		t.Run(d.table, func(t *testing.T) {
			rows := records(t, d.table,
				historyRow(d.key, d.field, `"old"`, `"2026-09-01 01:00:00"`),
				historyRow(d.key, d.field, `"new"`, `"2026-09-01T02:00:00.000001"`),
				historyRow(d.key, d.field, `"middle"`, `"2026-09-01 02:00:00"`))
			for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
				got, err := merge.Latest(context.Background(), d.table, []ingest.Record{rows[order[0]], rows[order[1]], rows[order[2]]})
				if err != nil {
					t.Fatal(err)
				}
				assertRows(t, d.table, got, rows[1:2])
			}
			current, err := merge.Latest(context.Background(), d.table, rows[1:2])
			if err != nil {
				t.Fatal(err)
			}
			got, err := merge.Latest(context.Background(), d.table, append(current, rows[0]))
			if err != nil {
				t.Fatal(err)
			}
			assertRows(t, d.table, got, rows[1:2])
		})
	}
}

func TestT04_18RejectsSameTimeConflictsEvenBehindANewerWinner(t *testing.T) {
	for _, d := range historyTables {
		rows := records(t, d.table,
			historyRow(d.key, d.field, `"new"`, `"2026-09-01 03:00:00"`),
			historyRow(d.key, d.field, `"conflict-a"`, `"2026-09-01 01:00:00"`),
			historyRow(d.key, d.field, `"conflict-b"`, `"2026-09-01T01:00:00.000000"`))
		for _, input := range [][]ingest.Record{rows, rows[1:], {rows[2], rows[0], rows[1]}} {
			got, err := merge.Latest(context.Background(), d.table, input)
			if !errors.Is(err, merge.ErrHistoryConflict) || got != nil {
				t.Fatal("conflict produced target")
			}
		}
		// Current source evidence and the next round use the same rule.
		current, err := merge.Latest(context.Background(), d.table, rows[1:2])
		if err != nil {
			t.Fatal(err)
		}
		if got, err := merge.Latest(context.Background(), d.table, append(current, rows[2])); !errors.Is(err, merge.ErrHistoryConflict) || got != nil {
			t.Fatal("cross-round conflict accepted")
		}
	}
}

func TestT04_19IdenticalRowsCollapseAndDetachTokens(t *testing.T) {
	for _, d := range historyTables {
		// A single unique source object needs no comparison clock.
		row := fmt.Sprintf(`{"%s":"001","%s":"same"}`, d.key, d.field)
		input := records(t, d.table, row, row)
		got, err := merge.Latest(context.Background(), d.table, input)
		if err != nil || len(got) != 1 {
			t.Fatal("identical history not collapsed")
		}
		input[0].Fields[d.field].Raw[1] = 'X'
		input[1].Fields[d.field].Raw[1] = 'X'
		assertRows(t, d.table, got, records(t, d.table, row))
	}
}

func TestT04_20LatestNullOrMissingNeverBackfillsOlderValues(t *testing.T) {
	for _, d := range historyTables {
		old := historyRow(d.key, d.field, `"old"`, `"2026-09-01 01:00:00"`)
		for _, latest := range []string{
			historyRow(d.key, d.field, `null`, `"2026-09-01 02:00:00"`),
			historyRow(d.key, d.field, `""`, `"2026-09-01 02:00:00"`),
			fmt.Sprintf(`{"%s":"001","ETL_TIME":"2026-09-01 02:00:00"}`, d.key),
		} {
			got, err := merge.Latest(context.Background(), d.table, records(t, d.table, latest, old))
			if err != nil {
				t.Fatal(err)
			}
			assertRows(t, d.table, got, records(t, d.table, latest))
		}
	}
}

func TestT04_21DistinctVersionsRequireComparableTimes(t *testing.T) {
	for _, d := range historyTables {
		valid := historyRow(d.key, d.field, `"valid"`, `"2026-09-01 02:00:00"`)
		for _, invalid := range []string{`null`, `""`, `"not-a-time"`, `"2026-02-30 00:00:00"`, `"2026-09-01T01:00:00Z"`, `"2026-09-01 01:00:00.0000001"`} {
			got, err := merge.Latest(context.Background(), d.table, records(t, d.table, valid, historyRow(d.key, d.field, `"invalid"`, invalid)))
			if !errors.Is(err, merge.ErrHistoryTime) || got != nil {
				t.Fatal("uncomparable history accepted")
			}
		}
		missing := fmt.Sprintf(`{"%s":"001"}`, d.key)
		if got, err := merge.Latest(context.Background(), d.table, records(t, d.table, valid, missing)); !errors.Is(err, merge.ErrHistoryTime) || got != nil {
			t.Fatal("missing time accepted")
		}
	}
	if _, err := merge.Latest(context.Background(), grades, records(t, grades, `{"ID":"1"}`)); !errors.Is(err, merge.ErrInvalid) {
		t.Fatal("history exception applied to ordinary source")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := merge.Latest(ctx, historyTables[0].table, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}
