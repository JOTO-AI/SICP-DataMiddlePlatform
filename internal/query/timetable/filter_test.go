package timetable_test

import (
	"encoding/json"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestT09_14UnscheduledExcludedAndMissingPlanFails(t *testing.T) {
	a := assembler(t)
	got, err := a.Student([]source.SourceRow{membership()}, nil, studentRequest())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("unscheduled %+v %v", got, err)
	}
	for _, relation := range []bool{true, false} {
		r, s := membership(), termSchedule()
		if relation {
			delete(r, "JHKCH")
		} else {
			delete(s, "JHKCH")
		}
		got, err = a.Student([]source.SourceRow{r}, []source.SourceRow{s}, studentRequest())
		if err == nil || got != nil {
			t.Fatalf("missing plan %+v %v", got, err)
		}
	}
}

func TestT09_15CourseFiltersApplyOnlyToLegalOwnArrangements(t *testing.T) {
	a := assembler(t, row(`{"KCH":"000C","KCMC":"000C 示例数学"}`))
	for _, tc := range []struct {
		query string
		count int
	}{{"000C", 1}, {"数学", 1}, {"00C", 1}, {"missing", 0}} {
		req := studentRequest()
		req.CourseQuery = &tc.query
		got, err := a.Student([]source.SourceRow{membership()}, []source.SourceRow{termSchedule()}, req)
		if err != nil || got == nil || len(got) != tc.count {
			t.Fatalf("%s got %+v %v", tc.query, got, err)
		}
	}
	a = assembler(t, row(`{"KCH":"000C","KCMC":"数学"}`))
	q := "00C"
	req := studentRequest()
	req.CourseQuery = &q
	got, err := a.Student([]source.SourceRow{membership()}, []source.SourceRow{termSchedule()}, req)
	if err != nil || len(got) != 0 {
		t.Fatalf("code substring %+v %v", got, err)
	}
	q = "数学"
	other := membership()
	other["XH"] = json.RawMessage(`"002"`)
	got, err = a.Student([]source.SourceRow{other}, []source.SourceRow{termSchedule()}, req)
	if err != nil || len(got) != 0 {
		t.Fatalf("name guessed ownership %+v %v", got, err)
	}
	bad := termSchedule()
	delete(bad, "SKRQ")
	q = "missing"
	if _, err = a.Student([]source.SourceRow{membership()}, []source.SourceRow{bad}, req); err == nil {
		t.Fatal("filter concealed malformed own schedule")
	}
}
