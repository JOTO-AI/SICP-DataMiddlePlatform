package candidate_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestVerifiedHistoricalShapesHaveFixedBusinessResults(t *testing.T) {
	raw, err := os.ReadFile("testdata/verified_business.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows           map[string][]source.SourceRow
		Year, Semester string
		Grade          contract.Grade
		Timetable      contract.TimetableEntry
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	o, h, at := onlineFixture(t, &candidateSource{rows: fixture.Rows})
	all := "all"
	student, err := o.Grades.Student(t.Context(), h, contract.StudentGradesRequest{StudentNumber: "S1", AcademicYear: &all}, at)
	if err != nil || student.Total != 1 || len(student.Items) != 1 || !reflect.DeepEqual(student.Items[0], fixture.Grade) {
		t.Fatal("student grades golden mismatch", err, student)
	}
	batch, err := o.Grades.Batch(t.Context(), h, contract.BatchGradesRequest{StudentNumbers: []string{"S1"}, AcademicYear: "all"}, at)
	if err != nil || !reflect.DeepEqual(batch, student) {
		t.Fatal("batch grades golden mismatch", err, batch)
	}
	schedule, err := o.Timetable.Student(t.Context(), h, contract.StudentTimetableRequest{StudentNumber: "S1", AcademicYear: fixture.Year, Semester: fixture.Semester}, at)
	if err != nil || len(schedule.Items) != 1 || !reflect.DeepEqual(schedule.Items[0], fixture.Timetable) {
		t.Fatal("student timetable golden mismatch", err, schedule)
	}
	staff, err := o.Timetable.Staff(t.Context(), h, contract.StaffTimetableRequest{StaffNumber: "T1", AcademicYear: fixture.Year, Semester: fixture.Semester}, at)
	if err != nil || !reflect.DeepEqual(staff.Items, schedule.Items) {
		t.Fatal("staff timetable golden mismatch", err, staff)
	}
}
