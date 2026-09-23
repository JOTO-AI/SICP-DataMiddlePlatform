package read_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type scanClock struct{ now time.Time }

func (c *scanClock) Now() time.Time { return c.now }
func sourceGrade(id string) source.SourceRow {
	b, _ := json.Marshal(id)
	return source.SourceRow{"ID": b}
}
func scanFixture(t *testing.T, rows []source.SourceRow, l read.Limits) (read.Matcher, *read.Budget, *scanClock, *int) {
	t.Helper()
	clock := &scanClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	budget, err := read.NewBudget(l, clock)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	r := matcherFunc(func(_ context.Context, h read.Handle, table, after string, limit int, _ []source.Filter) ([]source.SourceRow, error) {
		calls++
		if h != original || table != "dwd_bzkscjzlb" {
			t.Fatal(h, table)
		}
		result := []source.SourceRow{}
		for _, row := range rows {
			var id string
			_ = json.Unmarshal(row["ID"], &id)
			if id > after {
				result = append(result, row)
				if len(result) == limit {
					break
				}
			}
		}
		return result, nil
	})
	return r, budget, clock, &calls
}

func TestT06_09FilterAllBatchesBeforeCountingAndPaging(t *testing.T) {
	l := read.DefaultLimits()
	l.BatchSize = 2
	r, b, _, calls := scanFixture(t, []source.SourceRow{sourceGrade("01"), sourceGrade("02"), sourceGrade("03"), sourceGrade("04"), sourceGrade("05")}, l)
	filtered := []contract.Grade{}
	err := read.Scan(t.Context(), r, original, "dwd_bzkscjzlb", nil, b, func(row source.SourceRow) error {
		var id string
		_ = json.Unmarshal(row["ID"], &id)
		if id >= "03" {
			filtered = append(filtered, contract.Grade{GradeID: id, Student: contract.GradeStudent{StudentNumber: "001"}})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	total, page, err := compute.Grades(filtered, 2, 2)
	if err != nil || total != 3 || len(page) != 1 || page[0].GradeID != "05" || *calls != 3 {
		t.Fatal(total, page, err, *calls)
	}
}

func TestT06_15ScanBudgetsAtAndBeyondBoundary(t *testing.T) {
	for _, kind := range []string{"batches", "rows", "bytes", "duration"} {
		for _, over := range []bool{false, true} {
			name := kind + "/at"
			if over {
				name = kind + "/over"
			}
			t.Run(name, func(t *testing.T) {
				l := read.DefaultLimits()
				l.BatchSize = 2
				rows := []source.SourceRow{sourceGrade("01"), sourceGrade("02"), sourceGrade("03")}
				switch kind {
				case "batches":
					l.Batches = 2
					if over {
						l.Batches = 1
					}
				case "rows":
					l.Rows = 3
					if over {
						l.Rows = 2
					}
				case "bytes":
					l.Bytes = 18
					if over {
						l.Bytes = 17
					}
				}
				r, b, clock, calls := scanFixture(t, rows, l)
				if kind == "duration" {
					originalReader := r
					r = matcherFunc(func(c context.Context, h read.Handle, table, a string, n int, f []source.Filter) ([]source.SourceRow, error) {
						rows, err := originalReader.ReadMatching(c, h, table, a, n, f)
						clock.now = clock.now.Add(l.Duration)
						if over {
							clock.now = clock.now.Add(time.Nanosecond)
						}
						return rows, err
					})
					l.BatchSize = 4
					// One complete batch reaches the exact time boundary.
					l.Rows = 3
					l.BatchSize = 4
					b, _ = read.NewBudget(l, clock)
				}
				got, err := read.Collect(t.Context(), r, original, "dwd_bzkscjzlb", nil, b)
				if over {
					if !errors.Is(err, read.ErrBudget) || got != nil {
						t.Fatal(got, err)
					}
				} else if err != nil || len(got) != 3 {
					t.Fatal(got, err)
				}
				if over && kind == "batches" && *calls != 1 {
					t.Fatal("read after batch budget")
				}
			})
		}
	}
}

func TestT06_15SharedBudgetRejectsFurtherScansAndCorruptCursors(t *testing.T) {
	l := read.DefaultLimits()
	l.Rows = 1
	r, b, _, _ := scanFixture(t, []source.SourceRow{sourceGrade("01")}, l)
	if rows, err := read.Collect(t.Context(), r, original, "dwd_bzkscjzlb", nil, b); err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if rows, err := read.Collect(t.Context(), r, original, "dwd_bzkscjzlb", nil, b); !errors.Is(err, read.ErrBudget) || rows != nil {
		t.Fatal(rows, err)
	}
	for _, rows := range [][]source.SourceRow{{sourceGrade("02"), sourceGrade("01")}, {sourceGrade("01"), sourceGrade("01")}, {{"ID": json.RawMessage(`1`)}}} {
		_, b, _, _ := scanFixture(t, nil, read.DefaultLimits())
		r := matcherFunc(func(context.Context, read.Handle, string, string, int, []source.Filter) ([]source.SourceRow, error) {
			return rows, nil
		})
		if got, err := read.Collect(t.Context(), r, original, "dwd_bzkscjzlb", nil, b); !errors.Is(err, read.ErrOrder) || got != nil {
			t.Fatal(got, err)
		}
	}
}
