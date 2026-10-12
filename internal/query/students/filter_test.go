package students_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/students"
)

func fullPair() students.Pair {
	p := pair()
	p.Enrollment["XSLBM"] = json.RawMessage(`"432"`)
	p.Enrollment["XZ"] = json.RawMessage(`"3.0e0"`)
	p.Enrollment["RXNY"] = json.RawMessage(`"202609"`)
	p.Enrollment["YJBYRQ"] = json.RawMessage(`"20290630"`)
	return p
}

func TestT20_04StudentConversionsUseEnrollment(t *testing.T) {
	d := related()
	d.Majors[0]["XZ"] = json.RawMessage(`99`)
	got, err := assembler(t, d).Member(fullPair())
	if err != nil || got.StudyYears == nil || *got.StudyYears != 3 || *got.AdmissionMonth != "2026-09" || *got.ExpectedGraduationDate != "2029-06-30" || *got.StudentCategoryCode != "432" || *got.StudentCategoryName != "中高贯通生" {
		t.Fatal(got, err)
	}
	for _, tc := range []struct{ field, raw string }{
		{"XZ", `3.5`}, {"XZ", `2147483648`}, {"XZ", `"no"`}, {"RXNY", `"202613"`}, {"RXNY", `"2026-09-31"`}, {"YJBYRQ", `"20290229"`}, {"YJBYRQ", `"2029-06-30T00:00:00"`},
	} {
		p := fullPair()
		p.Enrollment[tc.field] = json.RawMessage(tc.raw)
		got, err := assembler(t, d).Member(p)
		if err == nil || !reflect.DeepEqual(got, contract.Student{}) {
			t.Fatal(tc, got, err)
		}
	}
	p := fullPair()
	p.Enrollment["XZ"] = json.RawMessage(`-1`)
	p.Enrollment["XSLBM"] = json.RawMessage(`"unknown"`)
	p.Enrollment["YJBYRQ"] = json.RawMessage(`"2028-02-29"`)
	got, err = assembler(t, d).Member(p)
	if err != nil || *got.StudyYears != -1 || got.StudentCategoryName != nil || *got.StudentCategoryCode != "unknown" || *got.ExpectedGraduationDate != "2028-02-29" {
		t.Fatal(got, err)
	}
}

func TestT20_05AllStudentFiltersIntersect(t *testing.T) {
	item, err := assembler(t, related()).Member(fullPair())
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"student_number":"001","name":"学","department_code":"D1","department_name":"院","campus_code":"X1","major_code":"Z1","grade":"2026","class_code":"B1","status_codes":["raw","other"],"enrollment_status_codes":["enrolled","other"],"on_campus_flag":"0","student_category_codes":["432","41"],"study_years":3,"admission_year":"2026","expected_graduation_year":"2029","counselor_staff_number":"T1"}`
	var req contract.StudentsRequest
	_ = json.Unmarshal([]byte(raw), &req)
	if !students.Match(item, req) {
		t.Fatal("full intersection rejected")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &fields)
	for field, value := range fields {
		t.Run(field, func(t *testing.T) {
			changed := json.RawMessage(`"missing"`)
			if len(value) > 0 && value[0] == '[' {
				changed = json.RawMessage(`["missing"]`)
			}
			if field == "study_years" {
				changed = json.RawMessage(`4`)
			}
			fields[field] = changed
			defer func() { fields[field] = value }()
			encoded, e := json.Marshal(fields)
			if e != nil {
				t.Fatal(e)
			}
			var request contract.StudentsRequest
			if e = json.Unmarshal(encoded, &request); e != nil {
				t.Fatal(e)
			}
			if students.Match(item, request) {
				t.Fatal("ignored filter", field)
			}
		})
	}
	if !students.Match(item, contract.StudentsRequest{}) {
		t.Fatal("default filtered status")
	}
	// Similar display strings do not change literal/case-sensitive matching.
	name := "%_"
	item.Name = "A%_学生"
	if !students.Match(item, contract.StudentsRequest{Name: &name}) {
		t.Fatal("literal substring")
	}
	name = "a"
	if students.Match(item, contract.StudentsRequest{Name: &name}) {
		t.Fatal("case folded")
	}
}

func TestT20_06StudentNullsStatesAndCurrentContacts(t *testing.T) {
	p := pair()
	d := related()
	d.Contacts[0]["DZXX"] = json.RawMessage(`null`)
	d.Contacts[0]["YDDH"] = json.RawMessage(`""`)
	got, err := assembler(t, d).Member(p)
	if err != nil || got.StudentCategoryCode != nil || got.StudentCategoryName != nil || got.StudyYears != nil || got.AdmissionMonth != nil || got.ExpectedGraduationDate != nil || got.Email != nil || got.Mobile != nil {
		t.Fatal(got, err)
	}
	for _, body := range []string{`{"study_years":3}`, `{"admission_year":"2026"}`, `{"expected_graduation_year":"2029"}`, `{"student_category_codes":["41"]}`, `{"status_codes":["enrolled"]}`, `{"enrollment_status_codes":["raw"]}`, `{"on_campus_flag":"1"}`} {
		var req contract.StudentsRequest
		_ = json.Unmarshal([]byte(body), &req)
		if students.Match(got, req) {
			t.Fatal("null or distinct state matched", body)
		}
	}
	req := contract.StudentsRequest{StatusCodes: []string{"raw"}, EnrollmentStatusCodes: []string{"enrolled"}}
	if !students.Match(got, req) {
		t.Fatal("independent states lost")
	}
	p = fullPair()
	d.Counselors = nil
	got, err = assembler(t, d).Member(p)
	code := "T1"
	if err != nil || !students.Match(got, contract.StudentsRequest{CounselorStaffNumber: &code}) {
		t.Fatal(got, err)
	}
}
