package grades_test

import (
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func text(v string) *string { return &v }
func terms() []source.SourceRow {
	return []source.SourceRow{
		row(`{"ID":"old1","XH":"001","XN":"2024-2025","XQM":"1","KCCJ":90}`),
		row(`{"ID":"old2","XH":"001","XN":"2024-2025","XQM":"2","KCCJ":90}`),
		row(`{"ID":"new1","XH":"001","XN":"2025-2026","XQM":"1","KCCJ":80}`),
		row(`{"ID":"new2","XH":"001","XN":"2025-2026","XQM":"2","KCCJ":70}`),
		row(`{"ID":"other","XH":"002","XN":"2026-2027","XQM":"1","KCCJ":99}`),
	}
}
func scopeIDs(t *testing.T, req contract.StudentGradesRequest) []string {
	t.Helper()
	rows, err := grades.StudentScope(terms(), req)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, r := range rows {
		id, err := compute.Required(r, "ID")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestT07_01LatestScopeBeforeScoreFilter(t *testing.T) {
	req := contract.StudentGradesRequest{StudentNumber: "001", GradeFilters: contract.GradeFilters{ScoreMin: number(80)}}
	if got := scopeIDs(t, req); !reflect.DeepEqual(got, []string{"new2"}) {
		t.Fatal(got)
	}
}
func TestT07_02ExplicitYearIncludesBothTerms(t *testing.T) {
	if got := scopeIDs(t, contract.StudentGradesRequest{StudentNumber: "001", AcademicYear: text("2024-2025")}); !reflect.DeepEqual(got, []string{"old1", "old2"}) {
		t.Fatal(got)
	}
}
func TestT07_03SemesterOnlyChoosesLatestApplicableYear(t *testing.T) {
	if got := scopeIDs(t, contract.StudentGradesRequest{StudentNumber: "001", GradeFilters: contract.GradeFilters{Semester: text("1")}}); !reflect.DeepEqual(got, []string{"new1"}) {
		t.Fatal(got)
	}
}
func TestT07_04AllYearsWithOptionalSemester(t *testing.T) {
	for _, tc := range []struct {
		semester *string
		want     []string
	}{{nil, []string{"old1", "old2", "new1", "new2"}}, {text("2"), []string{"old2", "new2"}}} {
		got := scopeIDs(t, contract.StudentGradesRequest{StudentNumber: "001", AcademicYear: text("all"), GradeFilters: contract.GradeFilters{Semester: tc.semester}})
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatal(got)
		}
	}
}
func TestT07_05StudentWithoutGradesDoesNotBorrowOtherTerms(t *testing.T) {
	if got := scopeIDs(t, contract.StudentGradesRequest{StudentNumber: "missing"}); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestT07_03NoRequestedSemesterAndInvalidSourceTerms(t *testing.T) {
	rows := []source.SourceRow{row(`{"XH":"001","XN":"2025-2026","XQM":"2"}`)}
	got, err := grades.StudentScope(rows, contract.StudentGradesRequest{StudentNumber: "001", GradeFilters: contract.GradeFilters{Semester: text("1")}})
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	for _, raw := range []string{`{"XH":"001","XN":"bad","XQM":"1"}`, `{"XH":"001","XN":"2025-2026","XQM":"3"}`} {
		if got, err := grades.StudentScope([]source.SourceRow{row(raw)}, contract.StudentGradesRequest{StudentNumber: "001"}); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
}
