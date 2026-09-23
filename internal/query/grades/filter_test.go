package grades_test

import (
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
)

func TestT07_13EveryCourseFilterMatchesItsOwnField(t *testing.T) {
	g, err := assembler(t, fixture()).Grade(base())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		yes, no contract.GradeFilters
	}{
		{"code", contract.GradeFilters{CourseCode: text("C01")}, contract.GradeFilters{CourseCode: text("C0")}},
		{"name", contract.GradeFilters{CourseName: text("ours")}, contract.GradeFilters{CourseName: text("course")}},
		{"attribute", contract.GradeFilters{CourseAttributeCodes: []string{"other", "unknown-attribute"}}, contract.GradeFilters{CourseAttributeCodes: []string{"other"}}},
		{"nature", contract.GradeFilters{CourseNatureCodes: []string{"unknown-nature"}}, contract.GradeFilters{CourseNatureCodes: []string{"unknown-type"}}},
		{"type", contract.GradeFilters{CourseTypeCodes: []string{"unknown-type"}}, contract.GradeFilters{CourseTypeCodes: []string{"unknown-nature"}}},
		{"department-code", contract.GradeFilters{CourseDepartmentCode: text("D02")}, contract.GradeFilters{CourseDepartmentCode: text("D01")}},
		{"department-name", contract.GradeFilters{CourseDepartmentName: text("Course")}, contract.GradeFilters{CourseDepartmentName: text("Student")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !grades.MatchCourse(g, tc.yes) || grades.MatchCourse(g, tc.no) {
				t.Fatal("incorrect filter", tc.name)
			}
		})
	}
	if !grades.MatchCourse(g, contract.GradeFilters{}) {
		t.Fatal("absent filters reject")
	}
}

func TestT07_13LiteralNameWildcardsAndMissingCourseFields(t *testing.T) {
	g := contract.Grade{Course: contract.Course{CourseCode: "001", CourseName: text(`100%_\ ' Course`)}}
	for _, tc := range []struct {
		query string
		want  bool
	}{{`%_\`, true}, {"'", true}, {"%Course", false}, {"course", false}} {
		if got := grades.MatchCourse(g, contract.GradeFilters{CourseName: text(tc.query)}); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	g.Course.CourseName = nil
	if grades.MatchCourse(g, contract.GradeFilters{CourseName: text("Course")}) || grades.MatchCourse(g, contract.GradeFilters{CourseDepartmentCode: text("D01")}) || grades.MatchCourse(g, contract.GradeFilters{CourseTypeCodes: []string{"1"}}) {
		t.Fatal("missing association matched specified filter")
	}
	if !grades.MatchCourse(g, contract.GradeFilters{CourseCode: text("001")}) {
		t.Fatal("missing course lost known code")
	}
}

func TestT07_14ArrayUnionAndConditionsIntersection(t *testing.T) {
	g, err := assembler(t, fixture()).Grade(base())
	if err != nil {
		t.Fatal(err)
	}
	f := contract.GradeFilters{CourseAttributeCodes: []string{"other", "unknown-attribute"}, CourseNatureCodes: []string{"unknown-nature", "other"}, CourseTypeCodes: []string{"unknown-type"}, CourseName: text("Course"), CourseDepartmentCode: text("D02")}
	if !grades.MatchCourse(g, f) {
		t.Fatal("union/intersection positive rejected")
	}
	f.CourseNatureCodes = []string{"other"}
	if grades.MatchCourse(g, f) {
		t.Fatal("one rejected condition accepted")
	}
	f.CourseNatureCodes = nil
	f.CourseDepartmentCode = text("D01")
	if grades.MatchCourse(g, f) {
		t.Fatal("student department substituted for course department")
	}
}
