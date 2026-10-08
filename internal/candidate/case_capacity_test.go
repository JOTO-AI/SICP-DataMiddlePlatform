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

type caseCapacityMatcher struct{}

func (caseCapacityMatcher) ReadMatching(_ context.Context, _ read.Handle, table, after string, limit int, _ []source.Filter) ([]source.SourceRow, error) {
	if table == timetable.SchedulesTable {
		if after != "" {
			return nil, nil
		}
		return []source.SourceRow{{"PKH": json.RawMessage(`"001"`), "KKXND": json.RawMessage(`"2025-2026"`), "KKXQM": json.RawMessage(`"1"`)}}, nil
	}
	if table != timetable.RelationsTable {
		return nil, source.ErrInput
	}
	start, _ := strconv.Atoi(after)
	var rows []source.SourceRow
	// 140000 distinct requests and one duplicate per identity, streamed in
	// bounded batches, exercise both the old row and dedup byte ceilings.
	for i := start + 1; i <= min(start+limit, 280000); i++ {
		rows = append(rows, source.SourceRow{"ID": json.RawMessage(fmt.Sprintf(`"%08d"`, i)), "XH": json.RawMessage(fmt.Sprintf(`"%08d"`, (i+1)/2)), "JXBH": json.RawMessage(`"class"`), "JHKCH": json.RawMessage(`"course"`)})
	}
	return rows, nil
}

func TestBT07_BackgroundEnumeratesMoreThan100000DistinctCases(t *testing.T) {
	o := Online{Source: caseCapacityMatcher{}, MaxCases: MaxBackgroundCases}
	b, err := read.NewBudget(read.BackgroundLimits(time.Minute), ports.SystemClock{})
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
	if err != nil || count != 140000 {
		t.Fatal("incomplete background coverage", count, err)
	}
}
