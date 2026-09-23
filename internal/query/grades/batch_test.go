package grades_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func TestT08_01BatchRequiresExplicitYear(t *testing.T) {
	if rows, err := grades.BatchScope(terms(), contract.BatchGradesRequest{}); err == nil || rows != nil {
		t.Fatal(rows, err)
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
