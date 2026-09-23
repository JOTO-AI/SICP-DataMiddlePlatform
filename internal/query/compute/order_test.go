package compute_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

func TestT06_10GradeOrderingAndStablePages(t *testing.T) {
	rows := []contract.Grade{{GradeID: "02", Student: contract.GradeStudent{StudentNumber: "001"}}, {GradeID: "01", Student: contract.GradeStudent{StudentNumber: "002"}}, {GradeID: "03", Student: contract.GradeStudent{StudentNumber: "001"}}}
	for _, indices := range [][]int{{0, 1, 2}, {2, 0, 1}, {1, 2, 0}, {2, 1, 0}} {
		input := []contract.Grade{rows[indices[0]], rows[indices[1]], rows[indices[2]]}
		before := append([]contract.Grade(nil), input...)
		ids := []string{}
		for page := 1; page <= 4; page++ {
			total, got, err := compute.Grades(input, page, 1)
			if err != nil || total != 3 {
				t.Fatal(total, err)
			}
			for _, r := range got {
				ids = append(ids, r.GradeID)
			}
		}
		if !reflect.DeepEqual(ids, []string{"02", "03", "01"}) || !reflect.DeepEqual(input, before) {
			t.Fatal(ids, input)
		}
	}
	if _, _, err := compute.Grades([]contract.Grade{rows[0], rows[0]}, 1, 1); err == nil {
		t.Fatal("duplicate identity accepted")
	}
}

func TestT06_10TimetableCompleteOrderHidesPKH(t *testing.T) {
	rows := []compute.Schedule{{PKH: "03", Entry: contract.TimetableEntry{CourseCode: "C"}}, {PKH: "01", Entry: contract.TimetableEntry{CourseCode: "A"}}, {PKH: "02", Entry: contract.TimetableEntry{CourseCode: "B"}}}
	for _, indices := range [][]int{{0, 1, 2}, {2, 0, 1}, {1, 2, 0}} {
		input := []compute.Schedule{rows[indices[0]], rows[indices[1]], rows[indices[2]]}
		got, err := compute.Timetable(input)
		if err != nil || len(got) != 3 || got[0].CourseCode != "A" || got[1].CourseCode != "B" || got[2].CourseCode != "C" {
			t.Fatal(got, err)
		}
		b, _ := json.Marshal(got)
		if strings.Contains(string(b), "PKH") || strings.Contains(string(b), "pkh") {
			t.Fatal(string(b))
		}
	}
	if _, err := compute.Timetable([]compute.Schedule{rows[0], rows[0]}); err == nil {
		t.Fatal("duplicate PKH accepted")
	}
}
