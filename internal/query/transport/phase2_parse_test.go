package transport_test

import (
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

type phase2ParseCase struct {
	op       contract.Operation
	fixture  string
	base     string
	pageSize int
	required []string
}

func phase2ParseCases() []phase2ParseCase {
	return []phase2ParseCase{
		{contract.Organizations, "organizations_request", `{}`, 1000, nil},
		{contract.Students, "students_request", `{}`, 1000, nil},
		{contract.Staff, "staff_request", `{}`, 1000, nil},
		{contract.StaffTeachingClasses, "staff_teaching_classes_request", `{"staff_number":"0002","academic_year":"2026-2027","semester":"1"}`, 100, []string{"staff_number", "academic_year", "semester"}},
		{contract.TeachingClassStudents, "teaching_class_students_request", `{"teaching_class_number":"005","planned_course_code":"004","academic_year":"2026-2027","semester":"1"}`, 100, []string{"teaching_class_number", "planned_course_code", "academic_year", "semester"}},
	}
}

func parsePhase2(t *testing.T, op contract.Operation, body string) (any, map[string]any) {
	t.Helper()
	value, err := transport.Parse(op, strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s: %s: %v", op, body, err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	return value, fields
}

func replacePhase2Field(t *testing.T, body, field string, value any) string {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatal(err)
	}
	fields[field] = value
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestT16_04Phase2DefaultsAndFullRequests(t *testing.T) {
	data, err := os.ReadFile("../contract/testdata/phase2.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]json.RawMessage
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, tc := range phase2ParseCases() {
		t.Run(string(tc.op), func(t *testing.T) {
			for _, body := range []string{tc.base, replacePhase2Field(t, replacePhase2Field(t, tc.base, "page", nil), "page_size", nil)} {
				_, fields := parsePhase2(t, tc.op, body)
				if fields["page"] != float64(1) || fields["page_size"] != float64(tc.pageSize) {
					t.Fatal(fields)
				}
			}
			_, actual := parsePhase2(t, tc.op, string(fixtures[tc.fixture]))
			var expected map[string]any
			if err := json.Unmarshal(fixtures[tc.fixture], &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("full request lost fields: %#v != %#v", actual, expected)
			}
			for field := range expected {
				required := false
				for _, name := range tc.required {
					required = required || name == field
				}
				if !required {
					parsePhase2(t, tc.op, replacePhase2Field(t, tc.base, field, nil))
				}
			}
			_, fields := parsePhase2(t, tc.op, replacePhase2Field(t, replacePhase2Field(t, tc.base, "page", 1000000), "page_size", 1000))
			if fields["page"] != float64(1000000) || fields["page_size"] != float64(1000) {
				t.Fatal(fields)
			}
		})
	}
	for _, state := range []bool{false, true} {
		value, _ := parsePhase2(t, contract.Organizations, replacePhase2Field(t, `{}`, "is_active", state))
		active := value.(contract.OrganizationsRequest).IsActive
		if active == nil || *active != state {
			t.Fatal("boolean filter lost")
		}
	}
	for _, body := range []string{`{}`, `{"is_active":null}`} {
		value, _ := parsePhase2(t, contract.Organizations, body)
		if value.(contract.OrganizationsRequest).IsActive != nil {
			t.Fatal("unset filter became false")
		}
	}
	value, _ := parsePhase2(t, contract.Students, `{"student_number":" 0001 ","status_codes":["08","01","08"]}`)
	student := value.(contract.StudentsRequest)
	if *student.StudentNumber != " 0001 " || !reflect.DeepEqual(student.StatusCodes, []string{"08", "01", "08"}) {
		t.Fatal("identifier or array changed before canonicalization")
	}
}

func TestT16_05Phase2StrictJSONAndBounds(t *testing.T) {
	for _, tc := range phase2ParseCases() {
		t.Run(string(tc.op), func(t *testing.T) {
			for _, body := range []string{"", `[]`, `null`, `true`, tc.base + `{}`, tc.base + `x`, tc.base[:len(tc.base)-1]} {
				assertInvalid(t, tc.op, body)
			}
			for _, suffix := range []string{`,"unknown":null}`, `,"Page":1}`, `,"page":1,"page":2}`, `,"page":1,"pa\u0067e":2}`} {
				prefix := strings.TrimSuffix(tc.base, "}")
				if prefix == "{" {
					suffix = strings.TrimPrefix(suffix, ",")
				}
				assertInvalid(t, tc.op, prefix+suffix)
			}
			for _, field := range []string{"page", "page_size"} {
				for _, bad := range []any{0, -1, 1000001, "1", true, []any{}, map[string]any{}, json.Number("1.0"), json.Number("1e0"), json.Number("9223372036854775808")} {
					assertInvalid(t, tc.op, replacePhase2Field(t, tc.base, field, bad))
				}
			}
			assertInvalid(t, tc.op, replacePhase2Field(t, tc.base, "page_size", 1001))
			for _, field := range tc.required {
				var missing map[string]any
				if err := json.Unmarshal([]byte(tc.base), &missing); err != nil {
					t.Fatal(err)
				}
				delete(missing, field)
				encoded, err := json.Marshal(missing)
				if err != nil {
					t.Fatal(err)
				}
				assertInvalid(t, tc.op, string(encoded))
				for _, bad := range []any{nil, "", " ", 1, true, []any{}, "x\n", "\ufffd"} {
					assertInvalid(t, tc.op, replacePhase2Field(t, tc.base, field, bad))
				}
			}
			for _, size := range []int{transport.MaxRequestBytes, transport.MaxRequestBytes + 1} {
				body := tc.base + strings.Repeat(" ", size-len(tc.base))
				if size == transport.MaxRequestBytes {
					parsePhase2(t, tc.op, body)
				} else {
					assertInvalid(t, tc.op, body)
				}
			}
			for _, body := range []io.Reader{nil, errorReader{}} {
				value, err := transport.Parse(tc.op, body)
				if value != nil || err == nil {
					t.Fatal("invalid reader accepted")
				}
			}
		})
	}
	for _, op := range []contract.Operation{contract.Organizations, contract.Students, contract.Staff} {
		field := "name"
		if op == contract.Organizations {
			field = "organization_name"
		}
		for _, bad := range []any{"", " ", "x\t", "\ufffd", 1, true, []any{}, strings.Repeat("a", 513)} {
			assertInvalid(t, op, replacePhase2Field(t, `{}`, field, bad))
		}
		parsePhase2(t, op, replacePhase2Field(t, `{}`, field, strings.Repeat("a", 512)))
		assertInvalid(t, op, `{"`+field+`":"\ud800"}`)
		assertInvalid(t, op, "{\""+field+"\":\"\xff\"}")
	}
	for _, tc := range []struct {
		op    contract.Operation
		field string
	}{
		{contract.Organizations, "category_codes"}, {contract.Students, "status_codes"}, {contract.Students, "enrollment_status_codes"}, {contract.Students, "student_category_codes"}, {contract.Staff, "status_codes"},
	} {
		for _, bad := range []any{[]any{}, []any{nil}, []any{1}, []any{""}, []any{" "}, []any{"x\n"}, "01", true} {
			assertInvalid(t, tc.op, replacePhase2Field(t, `{}`, tc.field, bad))
		}
		for _, size := range []int{1000, 1001} {
			body := `{"` + tc.field + `":[` + strings.TrimSuffix(strings.Repeat(`"01",`, size), ",") + `]}`
			if size == 1000 {
				parsePhase2(t, tc.op, body)
			} else {
				assertInvalid(t, tc.op, body)
			}
		}
	}
	for _, bad := range []any{"false", 0, 1, []any{}, map[string]any{}} {
		assertInvalid(t, contract.Organizations, replacePhase2Field(t, `{}`, "is_active", bad))
	}
}

func TestT16_06YearStudyYearsAndLegacyContracts(t *testing.T) {
	for _, field := range []string{"admission_year", "expected_graduation_year"} {
		for _, valid := range []string{"0001", "2026", "9999"} {
			parsePhase2(t, contract.Students, replacePhase2Field(t, `{}`, field, valid))
		}
		for _, bad := range []any{"0000", "026", "10000", "20a6", " 2026", "2026 ", "２０２６", 2026, "2026-09"} {
			assertInvalid(t, contract.Students, replacePhase2Field(t, `{}`, field, bad))
		}
	}
	for _, valid := range []int{1, 3, 2147483647} {
		parsePhase2(t, contract.Students, replacePhase2Field(t, `{}`, "study_years", valid))
	}
	for _, bad := range []any{0, -1, "3", true, json.Number("3.0"), json.Number("3e0"), json.Number("2147483648"), json.Number("99999999999999999999")} {
		assertInvalid(t, contract.Students, replacePhase2Field(t, `{}`, "study_years", bad))
	}
	for _, tc := range phase2ParseCases()[3:] {
		for _, bad := range []any{"all", "2026", "2026-2028", "999-1000", "0999-1000", "9999-10000", "2026-2027 ", 2026} {
			assertInvalid(t, tc.op, replacePhase2Field(t, tc.base, "academic_year", bad))
		}
		for _, valid := range []string{"1000-1001", "9998-9999"} {
			parsePhase2(t, tc.op, replacePhase2Field(t, tc.base, "academic_year", valid))
		}
		for _, bad := range []any{"0", "3", "01", 1, true} {
			assertInvalid(t, tc.op, replacePhase2Field(t, tc.base, "semester", bad))
		}
		parsePhase2(t, tc.op, replacePhase2Field(t, tc.base, "semester", "2"))
	}
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{
		{contract.StudentGrades, `{"student_number":"001"}`}, {contract.BatchGrades, `{"academic_year":"all"}`},
	} {
		_, fields := parsePhase2(t, tc.op, tc.body)
		if fields["page"] != float64(1) || fields["page_size"] != float64(100) {
			t.Fatal("legacy pagination changed")
		}
		for _, field := range []string{"is_active", "study_years", "admission_year"} {
			assertInvalid(t, tc.op, replacePhase2Field(t, tc.body, field, nil))
		}
	}
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{
		{contract.StudentTimetable, `{"student_number":"001","academic_year":"2026-2027","semester":"1"}`},
		{contract.StaffTimetable, `{"staff_number":"001","academic_year":"2026-2027","semester":"1"}`},
	} {
		parsePhase2(t, tc.op, tc.body)
		assertInvalid(t, tc.op, replacePhase2Field(t, tc.body, "page", 1))
		assertInvalid(t, tc.op, replacePhase2Field(t, tc.body, "academic_year", "all"))
	}
}
