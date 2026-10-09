package transport_test

import (
	"encoding/json"
	"fmt"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"strings"
	"testing"
)

func TestPrivateSmokeCasesValidateAllOperations(t *testing.T) {
	raw := `{"students.grades.query":[{"student_number":"synthetic-student"}],"students.grades.batch-query":[{"academic_year":"all","student_numbers":["synthetic-student"]}],"students.timetable.query":[{"student_number":"synthetic-student","academic_year":"2025-2026","semester":"1"}],"staff.timetable.query":[{"staff_number":"synthetic-teacher","academic_year":"2025-2026","semester":"1"}]}`
	cases, err := transport.ParseSmokeCases(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%v %#v", cases, cases), "synthetic") {
		t.Fatal("identifiers exposed")
	}
	for _, bad := range []string{"null", "{}", raw + "{}", strings.Replace(raw, `,"student_numbers":["synthetic-student"]`, ``, 1), strings.Replace(raw, `"semester":"1"`, `"semester":"3"`, 1), strings.Replace(raw, `"student_number":"synthetic-student"`, `"student_number":null`, 1)} {
		if _, err := transport.ParseSmokeCases(bad); err == nil {
			t.Fatal("invalid suite accepted")
		}
	}
	cases[contract.StudentGrades] = append(cases[contract.StudentGrades], cases[contract.StudentGrades][0])
	duplicate, _ := json.Marshal(cases)
	if _, err := transport.ParseSmokeCases(string(duplicate)); err == nil {
		t.Fatal("duplicate accepted")
	}
	cases[contract.StudentGrades] = nil
	for i := 0; i < 9; i++ {
		cases[contract.StudentGrades] = append(cases[contract.StudentGrades], json.RawMessage(fmt.Sprintf(`{"student_number":"S%d"}`, i)))
	}
	oversized, _ := json.Marshal(cases)
	if _, err := transport.ParseSmokeCases(string(oversized)); err == nil {
		t.Fatal("unbounded suite")
	}
}
