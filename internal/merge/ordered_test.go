package merge_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
)

func TestBT06_OrderedBaselineMatchesFullSort(t *testing.T) {
	random := rand.New(rand.NewPCG(73, 19))
	for attempt := 0; attempt < 12; attempt++ {
		var base, incoming []ingest.Record
		for i := 0; i < 45; i++ {
			base = append(base, records(t, grades, fmt.Sprintf(`{"ID":"%04d","XH":"old","XF":3.0}`, i))...)
			if random.IntN(4) == 0 {
				incoming = append(incoming, records(t, grades, fmt.Sprintf(`{"ID":"%04d","XH":null}`, i))...)
			}
		}
		random.Shuffle(len(incoming), func(i, j int) { incoming[i], incoming[j] = incoming[j], incoming[i] })
		o, in, reference := streamSetup(t, grades, ingest.Incremental, base, incoming)
		before, err := merge.Stream(t.Context(), o, &rowIterator{rows: base}, in, reference)
		if err != nil {
			t.Fatal(err)
		}
		o.OrderedBaseline = true
		actual := &candidate{}
		after, err := merge.Stream(t.Context(), o, &rowIterator{rows: base}, in, actual)
		if err != nil || before.SHA256 != after.SHA256 || before.Modified != after.Modified || after.Removed != 0 {
			t.Fatal("ordered merge differs", err)
		}
		assertRows(t, grades, actual.rows, reference.rows)
		if after.Buffers.SpoolBytesWritten >= before.Buffers.SpoolBytesWritten {
			t.Fatal("historical rows still spooled")
		}
	}
}
func TestBT06_OrderedBaselineRejectsDamageAndLateConflicts(t *testing.T) {
	for _, failure := range []string{"order", "digest", "duplicate"} {
		base := records(t, grades, `{"ID":"01"}`, `{"ID":"02"}`)
		o, in, sink := streamSetup(t, grades, ingest.Incremental, base, records(t, grades, `{"ID":"03"}`))
		o.OrderedBaseline = true
		switch failure {
		case "order":
			base[0], base[1] = base[1], base[0]
		case "digest":
			o.BaselineSHA256 = strings.Repeat("0", 64)
		case "duplicate":
			base[1] = base[0]
		}
		if _, err := merge.Stream(t.Context(), o, &rowIterator{rows: base}, in, sink); err == nil || sink.complete || !sink.aborted {
			t.Fatal(failure, err)
		}
	}
	for _, d := range historyTables {
		base := records(t, d.table, historyRow(d.key, d.field, `"newer"`, `"2026-09-01 02:00:00"`))
		for _, conflict := range []bool{false, true} {
			stamp := `"2026-09-01 01:00:00"`
			if conflict {
				stamp = `"2026-09-01 02:00:00"`
			}
			incoming := records(t, d.table, historyRow(d.key, d.field, `"older"`, stamp))
			o, in, sink := streamSetup(t, d.table, ingest.Incremental, base, incoming)
			o.OrderedBaseline = true
			o.Limits.BatchBytes = 512
			_, err := merge.Stream(t.Context(), o, &rowIterator{rows: base}, in, sink)
			if conflict {
				if !errors.Is(err, merge.ErrHistoryConflict) || sink.complete {
					t.Fatal("equal timestamp conflict", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				assertRows(t, d.table, sink.rows, base)
			}
		}
	}
}
