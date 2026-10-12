package timetable_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type groupedSource struct {
	rows  []source.SourceRow
	scans int
}

func (s *groupedSource) ReadMatching(_ context.Context, _ read.Handle, table, after string, n int, filters []source.Filter) ([]source.SourceRow, error) {
	if table != timetable.SchedulesTable {
		return nil, nil
	}
	if after == "" {
		s.scans++
	}
	var out []source.SourceRow
	for _, row := range s.rows {
		var key string
		_ = json.Unmarshal(row["PKH"], &key)
		if key <= after {
			continue
		}
		match := true
		for _, f := range filters {
			var value string
			_ = json.Unmarshal(row[f.Column], &value)
			match = match && value == f.Value
		}
		if match {
			out = append(out, row)
			if len(out) == n {
				break
			}
		}
	}
	return out, nil
}

func TestBT07StaffGroupMatchesReferenceAndSharesTermScan(t *testing.T) {
	r1 := termSchedule()
	r1["JSJGH"] = json.RawMessage(`"T001,T0010"`)
	r2 := termSchedule()
	r2["PKH"] = json.RawMessage(`"ZZ"`)
	r2["KKXQM"] = json.RawMessage(`"2"`)
	sourceRows := &groupedSource{rows: []source.SourceRow{r1, r2}}
	times, _ := compute.NewTimes("Asia/Shanghai")
	s := timetable.Service{Source: sourceRows, Times: times, Clock: ports.SystemClock{}, Limits: read.BackgroundLimits(time.Minute)}
	h := read.Handle{Position: storage.Position{Instance: storage.Main}, Generation: 1, Combination: "C1"}
	requests := []contract.StaffTimetableRequest{staffRequest(), staffRequest(), staffRequest(), staffRequest()}
	requests[1].StaffNumber = "T0010"
	requests[2].StaffNumber = "T00"
	requests[3].Semester = "2"
	at := time.Now()
	var want []contract.StaffTimetableResponse
	for _, req := range requests {
		response, err := s.Staff(t.Context(), h, req, at)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, response)
	}
	sourceRows.scans = 0
	var got []contract.StaffTimetableResponse
	err := s.StaffGroup(t.Context(), h, requests, at, func(i int, response contract.StaffTimetableResponse) error {
		if i != len(got) {
			t.Fatal("response reordered")
		}
		got = append(got, response)
		return nil
	})
	if err != nil || !reflect.DeepEqual(got, want) || sourceRows.scans != 2 {
		t.Fatal("batch differs or repeated scans", err, sourceRows.scans)
	}
	// A late malformed non-matching teacher must fail, not be skipped by a token index.
	r2["KKXQM"] = json.RawMessage(`"1"`)
	r2["JSJGH"] = json.RawMessage(`"illegal/@"`)
	emitted := 0
	if err := s.StaffGroup(t.Context(), h, requests[:1], at, func(int, contract.StaffTimetableResponse) error { emitted++; return nil }); err == nil || emitted != 0 {
		t.Fatal("invalid final row bypassed")
	}
	if _, err := s.Staff(t.Context(), h, requests[0], at); err == nil {
		t.Fatal("reference accepted invalid row")
	}
}

func TestBT07StaffGroupCapacityAndCancellationEmitNothing(t *testing.T) {
	rows := []source.SourceRow{}
	for _, key := range []string{"A", "B", "C", "D", "E"} {
		r := termSchedule()
		raw, _ := json.Marshal(key)
		r["PKH"] = raw
		wide, _ := json.Marshal(strings.Repeat("x", 7<<20))
		r["JXBMC"] = wide
		rows = append(rows, r)
	}
	times, _ := compute.NewTimes("Asia/Shanghai")
	limits := read.BackgroundLimits(time.Minute)
	limits.BatchSize = 1
	s := timetable.Service{Source: &groupedSource{rows: rows}, Times: times, Clock: ports.SystemClock{}, Limits: limits}
	h := read.Handle{Position: storage.Position{Instance: storage.Main}, Generation: 1, Combination: "C1"}
	emitted := 0
	emit := func(int, contract.StaffTimetableResponse) error { emitted++; return nil }
	if err := s.StaffGroup(t.Context(), h, []contract.StaffTimetableRequest{staffRequest()}, time.Now(), emit); !errors.Is(err, timetable.ErrGroupCapacity) || emitted != 0 {
		t.Fatal(err, emitted)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.StaffGroup(ctx, h, []contract.StaffTimetableRequest{staffRequest()}, time.Now(), emit); !errors.Is(err, context.Canceled) || emitted != 0 {
		t.Fatal(err, emitted)
	}
}
