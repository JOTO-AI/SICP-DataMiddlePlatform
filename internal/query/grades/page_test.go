package grades_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var confirmed = time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC)

func integer(v int) *int { return &v }

func TestT07_07SameCourseDistinctGradeIDsRemainSeparate(t *testing.T) {
	first, second := base(), base()
	second["ID"] = json.RawMessage(`"G02"`)
	got, err := assembler(t, fixture()).StudentPage([]source.SourceRow{second, first}, contract.StudentGradesRequest{StudentNumber: "001"}, confirmed)
	if err != nil || got.Total != 2 || len(got.Items) != 2 || got.Items[0].GradeID != "G01" || got.Items[1].GradeID != "G02" || got.Items[0].Course.CourseCode != got.Items[1].Course.CourseCode {
		t.Fatal(got, err)
	}
}

func TestT07_15StablePagesCountAfterEveryFilter(t *testing.T) {
	rows := []source.SourceRow{}
	for _, id := range []string{"G03", "G01", "G04", "G02"} {
		r := base()
		raw, _ := json.Marshal(id)
		r["ID"] = raw
		if id == "G04" {
			r["KCCJ"] = json.RawMessage(`70`)
		}
		rows = append(rows, r)
	}
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}} {
		input := []source.SourceRow{rows[order[0]], rows[order[1]], rows[order[2]], rows[order[3]]}
		ids := []string{}
		for page := 1; page <= 4; page++ {
			got, err := assembler(t, fixture()).StudentPage(input, contract.StudentGradesRequest{StudentNumber: "001", GradeFilters: contract.GradeFilters{ScoreMin: number(80), CourseCode: text("C01"), Page: integer(page), PageSize: integer(1)}}, confirmed)
			if err != nil || got.Total != 3 || got.SyncedAt != "2026-09-18T09:00:00+08:00" {
				t.Fatal(got, err)
			}
			for _, g := range got.Items {
				ids = append(ids, g.GradeID)
			}
			if page == 4 && len(got.Items) != 0 {
				t.Fatal("nonempty beyond last page")
			}
		}
		if !reflect.DeepEqual(ids, []string{"G01", "G02", "G03"}) {
			t.Fatal(ids)
		}
	}
}

func TestT07_05EmptyStudentAndInvalidSyncOrPage(t *testing.T) {
	a := assembler(t, fixture())
	got, err := a.StudentPage([]source.SourceRow{base()}, contract.StudentGradesRequest{StudentNumber: "none"}, confirmed)
	if err != nil || got.Total != 0 || len(got.Items) != 0 {
		t.Fatal(got, err)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"items":[]`) {
		t.Fatal(string(b))
	}
	for _, tc := range []struct {
		page int
		sync time.Time
	}{{0, confirmed}, {1, time.Time{}}} {
		got, err := a.StudentPage([]source.SourceRow{base()}, contract.StudentGradesRequest{StudentNumber: "001", GradeFilters: contract.GradeFilters{Page: integer(tc.page)}}, tc.sync)
		if err == nil || !reflect.DeepEqual(got, contract.GradesResponse{}) {
			t.Fatal(got, err)
		}
	}
}
