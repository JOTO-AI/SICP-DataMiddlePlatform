package candidate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type caseCapacityMatcher struct {
	calls, returned int
	duplicate       bool
}

func (m *caseCapacityMatcher) ReadMatching(_ context.Context, _ read.Handle, table, after string, limit int, _ []source.Filter) ([]source.SourceRow, error) {
	m.calls++
	if table == timetable.SchedulesTable {
		if after != "" {
			return nil, nil
		}
		m.returned++
		return []source.SourceRow{{"PKH": json.RawMessage(`"001"`), "KKXND": json.RawMessage(`"2025-2026"`), "KKXQM": json.RawMessage(`"1"`)}}, nil
	}
	if table != timetable.RelationsTable {
		return nil, source.ErrInput
	}
	start, _ := strconv.Atoi(after)
	var rows []source.SourceRow
	// Model 140000 identities with duplicate rows. Sampling must stop early,
	// without enumerating the population or reaching the old resource ceilings.
	for i := start + 1; i <= min(start+limit, 280000); i++ {
		id := (i + 1) / 2
		if m.duplicate {
			id = 1
		}
		rows = append(rows, source.SourceRow{"ID": json.RawMessage(fmt.Sprintf(`"%08d"`, i)), "XH": json.RawMessage(fmt.Sprintf(`"%08d"`, id)), "JXBH": json.RawMessage(`"class"`), "JHKCH": json.RawMessage(`"course"`)})
	}
	m.returned += len(rows)
	return rows, nil
}

func TestBusinessCaseDiscoveryStopsBeforeEnumeratingLargePopulation(t *testing.T) {
	matcher := &caseCapacityMatcher{}
	o := Online{Source: matcher}
	b, err := read.NewBudget(sampleLimits(), ports.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	h := read.Handle{Position: storage.Position{Instance: storage.Main}, Generation: 1, Combination: "C1"}
	count := 0
	err = o.cases(t.Context(), h, contract.StudentTimetable, b, func(request any) error {
		count++
		want := fmt.Sprintf("%08d", count)
		if request.(contract.StudentTimetableRequest).StudentNumber != want {
			t.Fatal("case skipped, reordered or duplicated")
		}
		return nil
	})
	if err != nil || count != sampleCases || matcher.calls > 2 || matcher.returned > sampleRows+1 {
		t.Fatal("unbounded sample discovery", count, matcher.calls, matcher.returned, err)
	}
}

// Match the production discovery batch size so reading stops at the window boundary.
func sampleLimits() read.Limits {
	l := read.BackgroundLimits(time.Minute)
	l.BatchSize = sampleRows
	return l
}

func TestBusinessCaseDiscoveryStopsOnDuplicateWindow(t *testing.T) {
	matcher := &caseCapacityMatcher{duplicate: true}
	o := Online{Source: matcher}
	b, _ := read.NewBudget(sampleLimits(), ports.SystemClock{})
	h := read.Handle{Position: storage.Position{Instance: storage.Main}, Generation: 1, Combination: "C1"}
	count := 0
	err := o.cases(t.Context(), h, contract.StudentTimetable, b, func(any) error { count++; return nil })
	if err != nil || count != 1 || matcher.calls != 2 || matcher.returned > sampleRows+1 {
		t.Fatal("duplicate population scanned past the bounded window", count, matcher.calls, matcher.returned, err)
	}
}

func TestBusinessCaseDiscoveryPropagatesVisitorFailureAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		matcher := &caseCapacityMatcher{}
		o := Online{Source: matcher}
		b, _ := read.NewBudget(sampleLimits(), ports.SystemClock{})
		h := read.Handle{Position: storage.Position{Instance: storage.Main}, Generation: 1, Combination: "C1"}
		ctx, stop := context.WithCancel(t.Context())
		err := o.cases(ctx, h, contract.StudentTimetable, b, func(any) error {
			if cancel {
				stop()
				return nil
			}
			return source.ErrInput
		})
		stop()
		want := source.ErrInput
		if cancel {
			want = context.Canceled
		}
		if err != want {
			t.Fatal(cancel, err)
		}
	}
}
