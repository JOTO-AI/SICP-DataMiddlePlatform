package timetable_test

import (
	"encoding/json"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func staffRequest() contract.StaffTimetableRequest {
	return contract.StaffTimetableRequest{StaffNumber: "T001", AcademicYear: "2026-2027", Semester: "1"}
}

func TestT10_01OnlyCompleteStaffIDsMatch(t *testing.T) {
	s := termSchedule()
	s["JSJGH"] = json.RawMessage(`"T001,T0010"`)
	for _, tc := range []struct {
		id string
		n  int
	}{{"T00", 0}, {"001", 0}, {"T001", 1}, {"T0010", 1}, {"T00100", 0}} {
		req := staffRequest()
		req.StaffNumber = tc.id
		got, err := timetable.StaffMatches([]source.SourceRow{s}, req)
		if err != nil || got == nil || len(got) != tc.n {
			t.Fatalf("%s %+v %v", tc.id, got, err)
		}
	}
}
func TestT10_05StaffSchedulesNeedNoStudentMembership(t *testing.T) {
	got, err := timetable.StaffMatches([]source.SourceRow{termSchedule()}, staffRequest())
	if err != nil || len(got) != 1 || len(got[0].Membership) != 0 {
		t.Fatalf("got %+v %v", got, err)
	}
}
func TestT10_06OtherTermsNeverFallback(t *testing.T) {
	for _, change := range []struct{ key, value string }{{"KKXND", `"2025-2026"`}, {"KKXQM", `"2"`}} {
		s := termSchedule()
		s[change.key] = json.RawMessage(change.value)
		got, err := timetable.StaffMatches([]source.SourceRow{s}, staffRequest())
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("got %+v %v", got, err)
		}
	}
	req := staffRequest()
	req.AcademicYear = "all"
	if _, err := timetable.StaffMatches(nil, req); err == nil {
		t.Fatal("all accepted")
	}
}
