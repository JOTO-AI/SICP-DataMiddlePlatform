package timetable_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func studentRequest() contract.StudentTimetableRequest {
	return contract.StudentTimetableRequest{StudentNumber: "001", AcademicYear: "2026-2027", Semester: "1"}
}
func membership() source.SourceRow {
	return row(`{"ID":"R1","XH":"001","JXBH":"000J","JHKCH":"000C","ETL_TIME":"2026-09-18 06:00:00"}`)
}
func termSchedule() source.SourceRow {
	r := scheduleRow()
	r["KKXND"] = json.RawMessage(`"2026-2027"`)
	r["KKXQM"] = json.RawMessage(`"1"`)
	return r
}

func TestT09_09StudentWithoutOwnMembershipGetsNoClassmatesCourses(t *testing.T) {
	r := membership()
	r["XH"] = json.RawMessage(`"002"`)
	got, err := timetable.StudentMatches([]source.SourceRow{r}, []source.SourceRow{termSchedule()}, studentRequest())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestT09_10BothKeysAndRequestedTermMustMatch(t *testing.T) {
	for _, change := range []struct{ key, value string }{{"JXBH", `"other-class"`}, {"JHKCH", `"other-course"`}, {"KKXND", `"2025-2026"`}, {"KKXQM", `"2"`}} {
		t.Run(change.key, func(t *testing.T) {
			s := termSchedule()
			s[change.key] = json.RawMessage(change.value)
			got, err := timetable.StudentMatches([]source.SourceRow{membership()}, []source.SourceRow{s}, studentRequest())
			if err != nil || len(got) != 0 {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
	got, err := timetable.StudentMatches([]source.SourceRow{membership()}, []source.SourceRow{termSchedule()}, studentRequest())
	if err != nil || len(got) != 1 || len(got[0].Membership) != 1 || string(got[0].Schedule["PKH"]) != `"P001"` {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestT09_13CrossTermReuseFailsWithoutPartialResult(t *testing.T) {
	for _, change := range []struct{ key, value string }{{"KKXND", `"2027-2028"`}, {"KKXQM", `"2"`}} {
		other := termSchedule()
		other["PKH"] = json.RawMessage(`"P002"`)
		other[change.key] = json.RawMessage(change.value)
		got, err := timetable.StudentMatches([]source.SourceRow{membership()}, []source.SourceRow{termSchedule(), other}, studentRequest())
		if !errors.Is(err, timetable.ErrAmbiguous) || got != nil {
			t.Fatalf("got %+v %v", got, err)
		}
	}
	bad := termSchedule()
	bad["KKXND"] = json.RawMessage(`"2026-2028"`)
	if _, err := timetable.StudentMatches([]source.SourceRow{membership()}, []source.SourceRow{bad}, studentRequest()); err == nil {
		t.Fatal("invalid source term")
	}
	request := studentRequest()
	request.AcademicYear = "all"
	if _, err := timetable.StudentMatches(nil, nil, request); err == nil {
		t.Fatal("invalid request term")
	}
}
