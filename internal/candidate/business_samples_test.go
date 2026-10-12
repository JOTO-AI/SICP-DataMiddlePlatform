package candidate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type sampleMatcherFunc func(string, string, int, []source.Filter) ([]source.SourceRow, error)

func (f sampleMatcherFunc) ReadMatching(_ context.Context, _ read.Handle, table, after string, n int, filters []source.Filter) ([]source.SourceRow, error) {
	return f(table, after, n, filters)
}
func sampleRow(values map[string]string) source.SourceRow {
	row := source.SourceRow{}
	for k, v := range values {
		row[k], _ = json.Marshal(v)
	}
	return row
}
func selectTestCases(t *testing.T, m read.Matcher, op contract.Operation) ([]any, error) {
	t.Helper()
	b, err := read.NewBudget(sampleLimits(), ports.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	h := read.Handle{Position: storage.Position{Instance: storage.Main}, Generation: 1, Combination: "C1"}
	var requests []any
	err = (Online{Source: m}).cases(t.Context(), h, op, b, func(req any) error { requests = append(requests, req); return nil })
	return requests, err
}

func TestBusinessGradeSamplesCoverLatestAllAndMultipleStudents(t *testing.T) {
	calls := 0
	m := sampleMatcherFunc(func(table, after string, n int, _ []source.Filter) ([]source.SourceRow, error) {
		calls++
		if table != grades.GradesTable || after != "" || n > sampleRows {
			t.Fatal(table, after, n)
		}
		var rows []source.SourceRow
		for i := 0; i < n; i++ {
			rows = append(rows, sampleRow(map[string]string{"ID": fmt.Sprintf("%05d", i), "XH": fmt.Sprintf("S%05d", i/2)}))
		}
		return rows, nil
	})
	requests, err := selectTestCases(t, m, contract.StudentGrades)
	if err != nil || len(requests) != sampleCases || calls != 1 {
		t.Fatal(len(requests), calls, err)
	}
	for i, request := range requests {
		req := request.(contract.StudentGradesRequest)
		if req.StudentNumber != fmt.Sprintf("S%05d", i/2) {
			t.Fatal(req)
		}
		if i%2 == 0 && (req.AcademicYear == nil || *req.AcademicYear != "all") {
			t.Fatal(req)
		}
		if i%2 == 1 && req.AcademicYear != nil {
			t.Fatal(req)
		}
	}
	calls = 0
	requests, err = selectTestCases(t, m, contract.BatchGrades)
	if err != nil || len(requests) != 5 || calls != 1 {
		t.Fatal(len(requests), calls, err)
	}
	if len(requests[4].(contract.BatchGradesRequest).StudentNumbers) != 4 {
		t.Fatal(requests[4])
	}
}

func TestBusinessSelectionBoundsUnmatchedRelationsAndNestedSchedules(t *testing.T) {
	for _, schedules := range []bool{false, true} {
		t.Run(fmt.Sprint(schedules), func(t *testing.T) {
			calls := 0
			m := sampleMatcherFunc(func(table, after string, n int, filters []source.Filter) ([]source.SourceRow, error) {
				calls++
				if after != "" || n > sampleRows {
					t.Fatal("continued discovery beyond first window", after, n)
				}
				var rows []source.SourceRow
				if table == timetable.RelationsTable {
					for i := 0; i < n; i++ {
						rows = append(rows, sampleRow(map[string]string{"ID": fmt.Sprintf("%05d", i), "XH": "S1", "JXBH": fmt.Sprintf("J%05d", i), "JHKCH": "C1"}))
					}
				} else if schedules {
					for i := 0; i < n; i++ {
						rows = append(rows, sampleRow(map[string]string{"PKH": fmt.Sprintf("%05d", i), "KKXND": "2025-2026", "KKXQM": "1"}))
					}
				}
				return rows, nil
			})
			requests, err := selectTestCases(t, m, contract.StudentTimetable)
			if err != nil {
				t.Fatal(err)
			}
			if schedules {
				if len(requests) != 1 || calls > 5 {
					t.Fatal("nested discovery exceeded total window", len(requests), calls)
				}
			} else if len(requests) != 0 || calls > sampleRows+1 {
				t.Fatal("unmatched relations enumerated without bound", len(requests), calls)
			}
		})
	}
}

func TestBusinessSelectionNeverHidesReadFailure(t *testing.T) {
	failure := errors.New("database read failed")
	m := sampleMatcherFunc(func(string, string, int, []source.Filter) ([]source.SourceRow, error) { return nil, failure })
	for _, definition := range contract.LegacyBusinessDefinitions() {
		if _, err := selectTestCases(t, m, definition.Operation); !errors.Is(err, failure) {
			t.Fatal(definition.Operation, err)
		}
	}
}

func TestTeacherDiscoverySkipsDocumentedNullTerms(t *testing.T) {
	m := sampleMatcherFunc(func(table, after string, n int, filters []source.Filter) ([]source.SourceRow, error) {
		return []source.SourceRow{
			sampleRow(map[string]string{"PKH": "1"}),
			sampleRow(map[string]string{"PKH": "2", "KKXND": " ", "KKXQM": "", "JSJGH": ""}),
			sampleRow(map[string]string{"PKH": "3", "KKXND": "2025-2026", "KKXQM": "1", "JSJGH": "T1"}),
		}, nil
	})
	requests, err := selectTestCases(t, m, contract.StaffTimetable)
	if err != nil || len(requests) != 1 {
		t.Fatal(len(requests), err)
	}
}
