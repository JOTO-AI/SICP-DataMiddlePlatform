package timetable_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestT09_11RepeatedMembershipDoesNotMultiplySchedule(t *testing.T) {
	r := membership()
	r["ID"] = json.RawMessage(`"R2"`)
	r["ETL_TIME"] = json.RawMessage(`"2026-09-19 06:00:00"`)
	matches, err := timetable.StudentMatches([]source.SourceRow{membership(), r}, []source.SourceRow{termSchedule()}, studentRequest())
	if err != nil {
		t.Fatal(err)
	}
	got, err := assembler(t).Entries(matches)
	if err != nil || len(got) != 1 || got[0].SourceUpdatedAt == nil || *got[0].SourceUpdatedAt != "2026-09-19T06:00:00+08:00" {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestT09_12DifferentArrangementsRemainInPKHOrder(t *testing.T) {
	second := termSchedule()
	second["PKH"] = json.RawMessage(`"P002"`)
	second["SKJSH"] = json.RawMessage(`"00999"`)
	second["SKRQ"] = json.RawMessage(`"20260928"`)
	matches, err := timetable.StudentMatches([]source.SourceRow{membership()}, []source.SourceRow{second, termSchedule()}, studentRequest())
	if err != nil {
		t.Fatal(err)
	}
	got, err := assembler(t).Entries(matches)
	if err != nil || len(got) != 2 || got[0].RoomCode == nil || *got[0].RoomCode != "00101" || got[1].RoomCode == nil || *got[1].RoomCode != "00999" || len(got[1].Dates) != 1 || got[1].Dates[0] != "2026-09-28" {
		t.Fatalf("got %+v %v", got, err)
	}
	if string(matches[0].Schedule["PKH"]) != `"P002"` {
		t.Fatal("mutated input order")
	}
	matches = append(matches, matches[0])
	got, err = assembler(t).Entries(matches)
	if !errors.Is(err, compute.ErrRecords) || got != nil {
		t.Fatalf("duplicate source identity %+v %v", got, err)
	}
}
