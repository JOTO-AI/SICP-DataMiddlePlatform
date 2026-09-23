package grades_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestT07_12InclusiveNumericRangeExcludesNullAndText(t *testing.T) {
	f := contract.GradeFilters{ScoreMin: number(80), ScoreMax: number(90)}
	for _, tc := range []struct {
		raw  string
		want bool
	}{{`79.9`, false}, {`80`, true}, {`90`, true}, {`90.1`, false}, {`null`, false}, {`"缺考"`, false}} {
		t.Run(tc.raw, func(t *testing.T) {
			r := base()
			r["KCCJ"] = json.RawMessage(tc.raw)
			g, err := assembler(t, grades.Related{}).Grade(r)
			if err != nil {
				t.Fatal(err)
			}
			if got := grades.MatchScore(g, f); got != tc.want {
				t.Fatal(tc.raw, got)
			}
			if !grades.MatchScore(g, contract.GradeFilters{}) {
				t.Fatal("absent bounds removed record")
			}
		})
	}
	for _, tc := range []struct {
		value float64
		f     contract.GradeFilters
		want  bool
	}{
		{0, contract.GradeFilters{ScoreMin: number(0), ScoreMax: number(0)}, true},
		{90.1, contract.GradeFilters{ScoreMin: number(80)}, true},
		{79.9, contract.GradeFilters{ScoreMax: number(90)}, true},
		{80, contract.GradeFilters{ScoreMin: number(90), ScoreMax: number(80)}, false},
	} {
		if got := grades.MatchScore(contract.Grade{Score: number(tc.value)}, tc.f); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}

func TestT07_01DefaultLatestTermCannotFallBackAfterFiltering(t *testing.T) {
	rows := terms()
	for _, r := range rows {
		r["KCH"] = json.RawMessage(`"C01"`)
	}
	a := assembler(t, fixture())
	req := contract.StudentGradesRequest{StudentNumber: "001", GradeFilters: contract.GradeFilters{ScoreMin: number(80)}}
	got, err := a.Student(rows, req)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	req.AcademicYear = text("all")
	got, err = a.Student(rows, req)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, g := range got {
		ids = append(ids, g.GradeID)
	}
	if !reflect.DeepEqual(ids, []string{"old1", "old2", "new1"}) {
		t.Fatal(ids)
	}
}

func TestT07_11UnboundedTextScoreRemainsInSelectedResults(t *testing.T) {
	r := base()
	r["KCCJ"] = json.RawMessage(`"缺考"`)
	got, err := assembler(t, fixture()).Student([]source.SourceRow{r}, contract.StudentGradesRequest{StudentNumber: "001"})
	if err != nil || len(got) != 1 || got[0].Score != nil {
		t.Fatal(got, err)
	}
}
