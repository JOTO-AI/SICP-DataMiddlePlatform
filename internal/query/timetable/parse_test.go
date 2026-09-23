package timetable_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func row(s string) source.SourceRow {
	var r source.SourceRow
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		panic(err)
	}
	return r
}
func calendarRow() source.SourceRow {
	return row(`{"SKRQ":"20260921,2026-09-07,20260907","XQJ":"星期一","SKJC":"2,01,2","ZC":"3,1,3","QSZ":1,"ZZZ":16}`)
}

func TestT09_01OnlyExplicitSparseDatesAndWeeks(t *testing.T) {
	got, err := timetable.ParseCalendar(calendarRow())
	want := timetable.Calendar{Weekday: 1, Sections: []int{1, 2}, Weeks: []int{1, 3}, Dates: []string{"2026-09-07", "2026-09-21"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestT09_02InvalidCalendarFailsWithoutPartialResult(t *testing.T) {
	cases := []struct{ field, raw string }{
		{"SKRQ", `"20260230"`}, {"SKRQ", `"2026-2-01"`}, {"SKRQ", `"00000101"`}, {"SKRQ", `"20260907,"`},
		{"SKRQ", `"20260907;20260921"`}, {"SKRQ", `null`}, {"XQJ", `"0"`}, {"XQJ", `"8"`}, {"XQJ", `1`},
		{"SKJC", `"1-2"`}, {"SKJC", `"1,,2"`}, {"SKJC", `"+1"`}, {"SKJC", `"0"`}, {"SKJC", `"1.5"`},
		{"SKJC", `"2147483648"`}, {"ZC", `"1/3"`}, {"ZC", `null`}, {"ZC", `""`},
	}
	for _, tc := range cases {
		t.Run(tc.field+tc.raw, func(t *testing.T) {
			r := calendarRow()
			r[tc.field] = json.RawMessage(tc.raw)
			got, err := timetable.ParseCalendar(r)
			if err == nil || !reflect.DeepEqual(got, timetable.Calendar{}) {
				t.Fatalf("partial %+v %v", got, err)
			}
		})
	}
	for _, field := range []string{"SKRQ", "XQJ", "SKJC", "ZC"} {
		r := calendarRow()
		delete(r, field)
		if _, err := timetable.ParseCalendar(r); err == nil {
			t.Fatalf("accepted missing %s", field)
		}
	}
}

func TestT09_03SectionHoursAreExplicitAndUnknownHoursAbsent(t *testing.T) {
	want := []timetable.SectionTime{{"08:20", "08:59"}, {"09:00", "09:40"}, {"09:50", "10:30"}, {"10:35", "11:10"}, {"11:15", "11:55"}, {"13:10", "13:49"}, {"13:50", "14:30"}, {"14:40", "15:19"}, {"15:20", "16:00"}, {"16:10", "16:49"}, {"16:50", "17:30"}, {"17:35", "18:15"}, {"19:00", "19:40"}}
	for i, v := range want {
		got, ok := timetable.SectionHours(i + 1)
		if !ok || got != v {
			t.Fatalf("section %d: %+v", i+1, got)
		}
	}
	for _, n := range []int{-1, 0, 14, 1000} {
		got, ok := timetable.SectionHours(n)
		if ok || got != (timetable.SectionTime{}) {
			t.Fatalf("invented %d: %+v", n, got)
		}
	}
	r := calendarRow()
	r["SKJC"] = json.RawMessage(`"14"`)
	r["XQJ"] = json.RawMessage(`"7"`)
	got, err := timetable.ParseCalendar(r)
	if err != nil || got.Weekday != 7 || !reflect.DeepEqual(got.Sections, []int{14}) {
		t.Fatalf("got %+v %v", got, err)
	}
}
