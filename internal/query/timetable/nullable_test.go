package timetable_test

import (
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"testing"
)

// Shapes mirror retained source samples; all identifiers are synthetic.
func TestDocumentedNullableScheduleFields(t *testing.T) {
	for _, raw := range []string{"", "null", `""`, `" "`} {
		r := scheduleRow()
		for _, key := range []string{"JHKCH", "JXBH", "SKRQ", "JSJGH", "JSXM", "XDRS"} {
			if raw == "" {
				delete(r, key)
			} else {
				r[key] = json.RawMessage(raw)
			}
		}
		got, err := assembler(t).Schedule(r)
		if err != nil || got.Entry.PlannedCourseCode != "" || got.Entry.TeachingClassNumber != "" || got.Entry.EnrolledCount != nil || got.Entry.Dates == nil || len(got.Entry.Dates) != 0 || len(got.Entry.TeacherStaffNumbers) != 0 {
			t.Fatalf("nullable fields failed: %v", err)
		}
	}
}

func TestNullableScheduleTermsAndMembershipDoNotInventOwnership(t *testing.T) {
	for _, key := range []string{"KKXND", "KKXQM"} {
		for _, raw := range []string{"null", `""`, `" "`} {
			missing := termSchedule()
			missing[key] = json.RawMessage(raw)
			got, err := timetable.StudentMatches([]source.SourceRow{membership()}, []source.SourceRow{missing, termSchedule()}, studentRequest())
			if err != nil || len(got) != 1 {
				t.Fatalf("student missing %s: %v", key, err)
			}
			staff, err := timetable.StaffMatches([]source.SourceRow{missing, termSchedule()}, contract.StaffTimetableRequest{StaffNumber: "001", AcademicYear: "2026-2027", Semester: "1"})
			if err != nil || len(staff) != 1 {
				t.Fatalf("staff missing %s: %v", key, err)
			}
		}
	}
	missing := membership()
	delete(missing, "JHKCH")
	got, err := timetable.StudentMatches([]source.SourceRow{missing}, []source.SourceRow{termSchedule()}, studentRequest())
	if err != nil || len(got) != 0 {
		t.Fatal("invented membership", err)
	}
}
