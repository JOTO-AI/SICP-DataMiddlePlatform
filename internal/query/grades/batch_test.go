package grades_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestT08_01BatchRequiresExplicitYear(t *testing.T) {
	if rows, err := grades.BatchScope(terms(), contract.BatchGradesRequest{}); err == nil || rows != nil {
		t.Fatal(rows, err)
	}
}

func TestT08_09BatchAndStudentShareFieldsFiltersAndMissingRelations(t *testing.T) {
	for _, missing := range []bool{false, true} {
		for _, filter := range []contract.GradeFilters{
			{}, {ScoreMin: number(80), ScoreMax: number(90)}, {ScoreMin: number(90)}, {CourseCode: text("C01")},
			{CourseName: text("Course")}, {CourseAttributeCodes: []string{"unknown-attribute"}}, {CourseDepartmentCode: text("D02")},
		} {
			data := fixture()
			if missing {
				data = grades.Related{}
			}
			a := assembler(t, data)
			rows := []source.SourceRow{base()}
			single, err := a.Student(rows, contract.StudentGradesRequest{StudentNumber: "001", AcademicYear: text("all"), GradeFilters: filter})
			if err != nil {
				t.Fatal(err)
			}
			batch, err := a.Batch(rows, contract.BatchGradesRequest{AcademicYear: "all", StudentNumbers: []string{"001", "001"}, GradeFilters: filter})
			if err != nil || !reflect.DeepEqual(batch, single) {
				t.Fatal(missing, filter, batch, single, err)
			}
		}
	}
	r := base()
	r["KCCJ"] = json.RawMessage(`"缺考"`)
	got, err := assembler(t, fixture()).Batch([]source.SourceRow{r}, contract.BatchGradesRequest{AcademicYear: "all"})
	if err != nil || len(got) != 1 || got[0].Score != nil || got[0].Credits == nil || *got[0].Credits != 2 {
		t.Fatal(got, err)
	}
}

func TestT08_03AllWithSemesterReturnsExactCrossYearSet(t *testing.T) {
	rows, err := grades.BatchScope(terms(), contract.BatchGradesRequest{AcademicYear: "all", GradeFilters: contract.GradeFilters{Semester: text("2")}})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, row := range rows {
		id, _ := compute.Required(row, "ID")
		ids = append(ids, id)
	}
	if !reflect.DeepEqual(ids, []string{"old2", "new2"}) {
		t.Fatal(ids)
	}
}

func TestT08_08OnlyLegalYearDoesNotRequireDepartmentOrClass(t *testing.T) {
	parsed, err := transport.Parse(contract.BatchGrades, strings.NewReader(`{"academic_year":"2025-2026"}`))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := grades.BatchScope(terms(), parsed.(contract.BatchGradesRequest))
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
}
func TestT08_02BatchUsesCommonYearInsteadOfPerStudentLatest(t *testing.T) {
	for _, tc := range []struct {
		year     string
		semester *string
		want     []string
	}{
		{"2024-2025", nil, []string{"old1", "old2"}},
		{"2025-2026", nil, []string{"new1", "new2"}},
		{"all", nil, []string{"old1", "old2", "new1", "new2", "other"}},
		{"all", text("1"), []string{"old1", "new1", "other"}},
	} {
		rows, err := grades.BatchScope(terms(), contract.BatchGradesRequest{AcademicYear: tc.year, GradeFilters: contract.GradeFilters{Semester: tc.semester}})
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, row := range rows {
			id, _ := compute.Required(row, "ID")
			ids = append(ids, id)
		}
		if !reflect.DeepEqual(ids, tc.want) {
			t.Fatal(tc, ids)
		}
	}
}

func TestT08_04RepeatedIDsAndSameNamesDoNotMergeOrMultiplyGrades(t *testing.T) {
	rows := []contract.Grade{{GradeID: "G01", Student: contract.GradeStudent{StudentNumber: "001", Name: text("Same")}}, {GradeID: "G02", Student: contract.GradeStudent{StudentNumber: "002", Name: text("Same")}}}
	req := contract.BatchGradesRequest{StudentNumbers: []string{"001", "001", "002"}}
	ids := []string{}
	for _, g := range rows {
		if grades.MatchStudent(g, req) {
			ids = append(ids, g.GradeID)
		}
	}
	if !reflect.DeepEqual(ids, []string{"G01", "G02"}) {
		t.Fatal(ids)
	}
}

func TestT08_05CurrentAffiliationFiltersIntersectWithStudentSet(t *testing.T) {
	g, err := assembler(t, fixture()).Grade(base())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		req  contract.BatchGradesRequest
		want bool
	}{
		{"all", contract.BatchGradesRequest{StudentNumbers: []string{"001"}, StudentDepartmentCode: text("D01"), StudentDepartmentName: text("Student"), ClassCode: text("B01"), ClassName: text("Class")}, true},
		{"other-student", contract.BatchGradesRequest{StudentNumbers: []string{"002"}}, false},
		{"old-dept", contract.BatchGradesRequest{StudentDepartmentCode: text("OLD")}, false},
		{"dept-name", contract.BatchGradesRequest{StudentDepartmentName: text("Course")}, false},
		{"class-code", contract.BatchGradesRequest{ClassCode: text("B02")}, false},
		{"class-name", contract.BatchGradesRequest{ClassName: text("Other")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := grades.MatchStudent(g, tc.req); got != tc.want {
				t.Fatal(got)
			}
		})
	}
	g.Student.DepartmentCode = nil
	if grades.MatchStudent(g, contract.BatchGradesRequest{StudentDepartmentCode: text("D01")}) {
		t.Fatal("missing affiliation matched")
	}
}

func TestT08_06StudentAndTeachingDepartmentsAreIndependent(t *testing.T) {
	g, err := assembler(t, fixture()).Grade(base())
	if err != nil {
		t.Fatal(err)
	}
	req := contract.BatchGradesRequest{StudentDepartmentCode: text("D01"), GradeFilters: contract.GradeFilters{CourseDepartmentCode: text("D02")}}
	if !grades.MatchStudent(g, req) || !grades.MatchCourse(g, req.GradeFilters) {
		t.Fatal("independent dimensions rejected")
	}
	req.StudentDepartmentCode = text("D02")
	if grades.MatchStudent(g, req) {
		t.Fatal("teaching department substituted")
	}
	req.StudentDepartmentCode = text("D01")
	req.CourseDepartmentCode = text("D01")
	if grades.MatchCourse(g, req.GradeFilters) {
		t.Fatal("student department substituted")
	}
}
