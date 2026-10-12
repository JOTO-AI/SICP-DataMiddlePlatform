package querymock

import (
	"fmt"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

const firstRoster = `[
 {"student_number":"00202600001","name":"虚构学生001","department_code":"00D1","department_name":"虚构单位1","class_code":"00B01","class_name":"虚构班级1","source_updated_at":"2026-08-05T07:50:00+08:00"},
 {"student_number":"00202600008","name":"虚构学生008","department_code":"00D2","department_name":"虚构单位2","class_code":"00B04","class_name":"虚构班级4","source_updated_at":"2026-08-05T07:50:00+08:00"},
 {"student_number":"00202600009","name":"虚构学生009","department_code":"00D1","department_name":"虚构单位1","class_code":"00B01","class_name":"虚构班级1","source_updated_at":"2026-08-05T07:50:00+08:00"}
]`

func Rosters(t *testing.T, q Query) {
	t.Helper()
	exact(t, q, contract.TeachingClassStudents, `{"teaching_class_number":"J2026-0001","planned_course_code":"000C0001","academic_year":"2026-2027","semester":"1"}`, firstRoster)
	// Fixed expectations reviewed from the 24 original membership records.
	// In particular class 1 includes student 8, not every member of admin class 1.
	members := [][]int{{1, 8, 9}, {1, 2, 9, 10}, {2, 3, 10, 11}, {3, 4, 11, 12}, {4, 5, 12}, {5, 6}, {6, 7}, {7, 8}}
	classCounts := map[string]int{}
	for _, teacher := range []struct {
		id      string
		classes []int
	}{{"001", []int{1, 7}}, {"1001", []int{1, 2, 8}}, {"T00003", []int{3}}, {"T00004", []int{4}}, {"T00005", []int{5}}, {"T00006", []int{6}}} {
		body := fmt.Sprintf(`{"staff_number":%q,"academic_year":"2026-2027","semester":"1"}`, teacher.id)
		data, _ := q(contract.StaffTeachingClasses, body, "")
		echo(t, data, contract.StaffTeachingClasses, body)
		classes := items(t, data, len(teacher.classes))
		if len(classes) != len(teacher.classes) {
			t.Fatal("incomplete teacher classes", data)
		}
		for index, value := range classes {
			row, ok := value.(map[string]any)
			if !ok {
				t.Fatal("invalid class item", value)
			}
			class, ok := row["teaching_class_number"].(string)
			if !ok {
				t.Fatal("missing class identity", row)
			}
			if class != fmt.Sprintf("J2026-%04d", teacher.classes[index]) {
				t.Fatal("wrong teacher association", teacher.id, row)
			}
			// Check the class/course pair, rather than accepting a course from output.
			found := false
			for i, ids := range members {
				if class != fmt.Sprintf("J2026-%04d", i+1) {
					continue
				}
				found = true
				if row["planned_course_code"] != fmt.Sprintf("000C%04d", i+1) || row["student_count"] != float64(len(ids)) {
					t.Fatal("class course/count differs from source baseline", row)
				}
				classCounts[class] = len(ids)
			}
			if !found {
				t.Fatal("unexpected class", row)
			}
		}
	}
	if len(classCounts) != 8 {
		t.Fatal("missing class combinations", classCounts)
	}
	for i, ids := range members {
		class := fmt.Sprintf("J2026-%04d", i+1)
		body := fmt.Sprintf(`{"teaching_class_number":%q,"planned_course_code":"000C%04d","academic_year":"2026-2027","semester":"1"}`, class, i+1)
		expected := []string{}
		for _, id := range ids {
			expected = append(expected, fmt.Sprintf("002026%05d", id))
		}
		pages(t, q, contract.TeachingClassStudents, body, "student_number", expected)
		data, _ := q(contract.TeachingClassStudents, body, "")
		echo(t, data, contract.TeachingClassStudents, body)
		if len(items(t, data, classCounts[class])) != classCounts[class] {
			t.Fatal("class count differs from roster", data)
		}
	}
}
