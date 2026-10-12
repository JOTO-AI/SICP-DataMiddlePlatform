package cache_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func TestT17_01Phase2KeysCanonicalAndIsolated(t *testing.T) {
	cases := []struct {
		op               contract.Operation
		base, equivalent string
		typed            any
	}{
		{contract.Organizations, `{}`, `{"page":1,"page_size":1000,"category_codes":null,"is_active":null}`, contract.OrganizationsRequest{}},
		{contract.Students, `{}`, `{"page":1,"page_size":1000,"student_number":null,"study_years":null}`, contract.StudentsRequest{}},
		{contract.Staff, `{}`, `{"page":1,"page_size":1000,"status_codes":null}`, contract.StaffRequest{}},
		{contract.StaffTeachingClasses, `{"staff_number":"001","academic_year":"2026-2027","semester":"1"}`, `{"staff_number":"001","academic_year":"2026-2027","semester":"1","page":1,"page_size":100,"course_code":null}`, contract.StaffTeachingClassesRequest{StaffNumber: "001", AcademicYear: "2026-2027", Semester: "1"}},
		{contract.TeachingClassStudents, `{"teaching_class_number":"002","planned_course_code":"003","academic_year":"2026-2027","semester":"1"}`, `{"teaching_class_number":"002","planned_course_code":"003","academic_year":"2026-2027","semester":"1","page":1,"page_size":100}`, contract.TeachingClassStudentsRequest{TeachingClassNumber: "002", PlannedCourseCode: "003", AcademicYear: "2026-2027", Semester: "1"}},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		t.Run(string(tc.op), func(t *testing.T) {
			base := key(t, "C1", tc.op, tc.base)
			if seen[base] {
				t.Fatal("operations share cache key")
			}
			seen[base] = true
			if !strings.HasPrefix(base, "query:C1:"+string(tc.op)+":") {
				t.Fatal(base)
			}
			if key(t, "C1", tc.op, tc.equivalent) != base {
				t.Fatal("null/default mismatch")
			}
			typed, err := cache.Key("C1", tc.op, tc.typed)
			if err != nil || typed != base {
				t.Fatal("typed defaults mismatch", typed, err)
			}
			for _, changed := range []string{strings.TrimSuffix(tc.base, "}") + `,"page":2}`, strings.TrimSuffix(tc.base, "}") + `,"page_size":5}`} {
				changed = strings.Replace(changed, "{,", "{", 1)
				if key(t, "C1", tc.op, changed) == base {
					t.Fatal("pagination collision")
				}
			}
			if key(t, "C2", tc.op, tc.base) == base {
				t.Fatal("bundle collision")
			}
			for _, other := range cases {
				if other.op != tc.op {
					if _, err := cache.Key("C1", tc.op, other.typed); err == nil {
						t.Fatal("wrong request type accepted")
					}
				}
			}
		})
	}
	for _, tc := range []struct {
		op    contract.Operation
		field string
	}{
		{contract.Organizations, "category_codes"}, {contract.Students, "status_codes"}, {contract.Students, "enrollment_status_codes"}, {contract.Students, "student_category_codes"}, {contract.Staff, "status_codes"},
	} {
		first := `{"` + tc.field + `":["02","01","02"]}`
		second := `{"` + tc.field + `":["01","02"]}`
		if key(t, "C1", tc.op, first) != key(t, "C1", tc.op, second) {
			t.Fatal("set not canonical", tc.field)
		}
	}
	req, err := transport.Parse(contract.Students, strings.NewReader(`{"status_codes":["02","01","02"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Key("C1", contract.Students, req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req.(contract.StudentsRequest).StatusCodes, []string{"02", "01", "02"}) {
		t.Fatal("key mutated caller slice")
	}
	falseKey := key(t, "C1", contract.Organizations, `{"is_active":false}`)
	if falseKey != "query:C1:organizations.query:is_active=false&page=1&page_size=1000" || falseKey == key(t, "C1", contract.Organizations, `{}`) || falseKey == key(t, "C1", contract.Organizations, `{"is_active":true}`) {
		t.Fatal(falseKey)
	}
	if key(t, "C1", contract.Students, `{"student_number":"001"}`) == key(t, "C1", contract.Students, `{"student_number":"1"}`) {
		t.Fatal("leading zero lost")
	}
	if !strings.Contains(key(t, "C1", contract.Students, `{"study_years":2147483647}`), "study_years=2147483647") {
		t.Fatal("integer value changed")
	}
}
