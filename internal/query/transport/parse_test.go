package transport_test

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func assertInvalid(t *testing.T, op contract.Operation, body string) {
	t.Helper()
	v, err := transport.Parse(op, strings.NewReader(body))
	var failure *transport.Failure
	if v != nil || !errors.As(err, &failure) || failure.Category != contract.InvalidArgument {
		t.Fatalf("accepted %q: %#v %v", body, v, err)
	}
}

func TestT06_02StrictJSON(t *testing.T) {
	for name, body := range map[string]string{
		"empty": "", "truncated": `{"student_number":"01"`, "duplicate": `{"student_number":"01","student_number":"02"}`,
		"escaped-duplicate": `{"student_number":"01","student_\u006eumber":"02"}`,
		"unknown":           `{"student_number":"01","extra":null}`, "case": `{"Student_Number":"01"}`,
		"tail": `{"student_number":"01"}{}`, "tail-garbage": `{"student_number":"01"}x`,
		"array": `[]`, "null": `null`, "scalar": `"01"`, "nested": `{"student_number":{"id":"01"}}`,
		"invalid-utf8": "{\"student_number\":\"\xff\"}", "surrogate": `{"student_number":"\ud800"}`,
	} {
		t.Run(name, func(t *testing.T) { assertInvalid(t, contract.StudentGrades, body) })
	}
	if _, err := transport.Parse(contract.StudentGrades, errorReader{}); err == nil {
		t.Fatal("read error ignored")
	}
	if _, err := transport.Parse(contract.StudentGrades, nil); err == nil {
		t.Fatal("nil body accepted")
	}
	assertInvalid(t, "unknown", `{}`)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestT06_03ParameterBounds(t *testing.T) {
	for name, field := range map[string]string{
		"number-id": `"student_number":1`, "null-id": `"student_number":null`, "empty-id": `"student_number":""`,
		"semester": `"semester":"3"`, "semester-number": `"semester":1`, "bad-year": `"academic_year":"2025"`, "year-order": `"academic_year":"2025-2027"`,
		"zero-page": `"page":0`, "negative-page": `"page":-1`, "decimal-page": `"page":1.0`, "string-page": `"page":"1"`,
		"large-page": `"page":1000001`, "zero-size": `"page_size":0`, "large-size": `"page_size":1001`,
		"inverted-range": `"score_min":90,"score_max":80`, "score-string": `"score_min":"80"`, "score-overflow": `"score_min":1e999`,
		"null-element": `"course_type_codes":[null]`, "numeric-element": `"course_type_codes":[1]`, "empty-element": `"course_type_codes":[""]`,
		"boolean": `"course_name":true`, "control": `"course_name":"x\n"`,
	} {
		t.Run(name, func(t *testing.T) {
			prefix := `"student_number":"01",`
			if strings.Contains(field, `"student_number"`) {
				prefix = ""
			}
			assertInvalid(t, contract.StudentGrades, "{"+prefix+field+"}")
		})
	}
	for _, op := range []contract.Operation{contract.StudentGrades, contract.BatchGrades, contract.StudentTimetable, contract.StaffTimetable} {
		t.Run(string(op)+"/required", func(t *testing.T) { assertInvalid(t, op, `{}`) })
	}
	for _, field := range []string{`"student_number":"01","semester":"1"`, `"student_number":"01","academic_year":"2025-2026"`, `"student_number":"01","academic_year":"all","semester":"1"`} {
		assertInvalid(t, contract.StudentTimetable, "{"+field+"}")
	}
}

func TestT06_03RequestStringAndArrayLimits(t *testing.T) {
	for _, size := range []int{transport.MaxStringBytes, transport.MaxStringBytes + 1} {
		body := `{"student_number":"` + strings.Repeat("0", size) + `"}`
		_, err := transport.Parse(contract.StudentGrades, strings.NewReader(body))
		if (err == nil) != (size == transport.MaxStringBytes) {
			t.Fatal(size, err)
		}
	}
	for _, size := range []int{transport.MaxArrayItems, transport.MaxArrayItems + 1} {
		body := `{"academic_year":"all","student_numbers":[` + strings.TrimSuffix(strings.Repeat(`"01",`, size), ",") + `]}`
		_, err := transport.Parse(contract.BatchGrades, strings.NewReader(body))
		if (err == nil) != (size == transport.MaxArrayItems) {
			t.Fatal(size, err)
		}
	}
	base := `{"student_number":"01"}`
	for _, size := range []int{transport.MaxRequestBytes, transport.MaxRequestBytes + 1} {
		body := base + strings.Repeat(" ", size-len(base))
		_, err := transport.Parse(contract.StudentGrades, strings.NewReader(body))
		if (err == nil) != (size == transport.MaxRequestBytes) {
			t.Fatal(size, err)
		}
	}
}

func TestT06_04OptionalFieldsAndDefaults(t *testing.T) {
	// Each optional field is checked alone, so one rejected sibling cannot hide
	// an accidentally accepted empty value or an incorrectly required field.
	for _, op := range []contract.Operation{contract.StudentGrades, contract.BatchGrades, contract.StudentTimetable, contract.StaffTimetable} {
		base := map[string]any{"academic_year": "2025-2026", "semester": "1"}
		if op == contract.StaffTimetable {
			base["staff_number"] = "001"
		} else if op != contract.BatchGrades {
			base["student_number"] = "001"
		}
		stringsFields := []string{"course_query"}
		arrays := []string{}
		if op == contract.StaffTimetable {
			stringsFields = append(stringsFields, "teaching_class_number")
		}
		if op == contract.StudentGrades || op == contract.BatchGrades {
			stringsFields = []string{"semester", "course_code", "course_name", "course_department_code", "course_department_name"}
			arrays = []string{"course_attribute_codes", "course_nature_codes", "course_type_codes"}
			if op == contract.StudentGrades {
				stringsFields = append(stringsFields, "academic_year")
			} else {
				stringsFields = append(stringsFields, "student_department_code", "student_department_name", "class_code", "class_name")
				arrays = append(arrays, "student_numbers")
			}
		}
		for _, field := range append(stringsFields, arrays...) {
			t.Run(string(op)+"/"+field, func(t *testing.T) {
				m := map[string]any{}
				for k, v := range base {
					m[k] = v
				}
				for _, value := range []any{nil, "", []string{}} {
					m[field] = value
					b, _ := json.Marshal(m)
					_, err := transport.Parse(op, strings.NewReader(string(b)))
					if (err == nil) != (value == nil) {
						t.Fatalf("%s: %v", b, err)
					}
				}
			})
		}
	}
	for _, body := range []string{`{"student_number":"01"}`, `{"student_number":"01","page":null,"page_size":null,"score_min":null,"score_max":null}`} {
		v, err := transport.Parse(contract.StudentGrades, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r := v.(contract.StudentGradesRequest)
		if *r.Page != 1 || *r.PageSize != 100 || r.ScoreMin != nil || r.ScoreMax != nil {
			t.Fatal(r)
		}
	}
}

func TestT06_05PreserveTextIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{
		{contract.StudentGrades, `{"student_number":" 0001 ","course_code":"0002","academic_year":"all","semester":"2","score_min":0,"score_max":0,"page":1000000,"page_size":1000}`},
		{contract.BatchGrades, `{"academic_year":"all","student_numbers":["0001"],"class_code":"0003"}`},
		{contract.StudentTimetable, `{"student_number":"0001","academic_year":"2025-2026","semester":"1"}`},
		{contract.StaffTimetable, `{"staff_number":"0001","academic_year":"2025-2026","semester":"1","teaching_class_number":"0004"}`},
	} {
		v, err := transport.Parse(tc.op, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(tc.op, err)
		}
		b, _ := json.Marshal(v)
		var want, got map[string]json.RawMessage
		_ = json.Unmarshal([]byte(tc.body), &want)
		_ = json.Unmarshal(b, &got)
		for key, value := range want {
			if string(got[key]) != string(value) {
				t.Fatal(key, string(got[key]), string(value))
			}
		}
	}
}
