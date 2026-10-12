package contract_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

func assertPhase2JSON(t *testing.T, value any, want []byte) {
	t.Helper()
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual any
	if err := json.Unmarshal(want, &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("contract fields or values changed\nwant: %s\ngot: %s", want, out)
	}
}

// Expectations are hand-written from the API field tables, never generated
// from these DTOs or the future query implementation.
func TestT16_01FullDTOFieldAndValueRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/phase2.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]json.RawMessage
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	cases := map[string]any{
		"organizations_request":            &contract.OrganizationsRequest{},
		"students_request":                 &contract.StudentsRequest{},
		"staff_request":                    &contract.StaffRequest{},
		"staff_teaching_classes_request":   &contract.StaffTeachingClassesRequest{},
		"teaching_class_students_request":  &contract.TeachingClassStudentsRequest{},
		"organizations_response":           &contract.OrganizationsResponse{},
		"students_response":                &contract.StudentsResponse{},
		"staff_response":                   &contract.StaffResponse{},
		"staff_teaching_classes_response":  &contract.StaffTeachingClassesResponse{},
		"teaching_class_students_response": &contract.TeachingClassStudentsResponse{},
	}
	if len(fixtures) != len(cases) {
		t.Fatalf("expected %d independent fixtures, got %d", len(cases), len(fixtures))
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			if err := json.Unmarshal(fixtures[name], target); err != nil {
				t.Fatal(err)
			}
			assertPhase2JSON(t, target, fixtures[name])
		})
	}
}

func TestT16_02NullEmptyFalseAndZero(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"organizations", contract.OrganizationsResponse{}, `{"total":0,"synced_at":"","items":[]}`},
		{"students", contract.StudentsResponse{}, `{"total":0,"synced_at":"","items":[]}`},
		{"staff", contract.StaffResponse{}, `{"total":0,"synced_at":"","items":[]}`},
		{"classes", contract.StaffTeachingClassesResponse{}, `{"staff_number":"","academic_year":"","semester":"","total":0,"synced_at":"","items":[]}`},
		{"members", contract.TeachingClassStudentsResponse{}, `{"teaching_class_number":"","planned_course_code":"","academic_year":"","semester":"","total":0,"synced_at":"","items":[]}`},
		{"class", contract.TeachingClass{}, `{"teaching_class_number":"","teaching_class_name":null,"planned_course_code":"","course_code":"","course_name":null,"teachers":[],"student_count":0,"source_updated_at":null}`},
		{"organization", contract.Organization{}, `{"organization_code":"","organization_name":"","parent_organization_code":null,"category_code":null,"category_name":null,"is_active":false,"source_updated_at":null}`},
		{"department", contract.Department{OrganizationCode: "009"}, `{"organization_code":"009","organization_name":null,"parent_organization_code":null,"category_code":null,"category_name":null,"is_active":null}`},
		{"student", contract.Student{}, `{"student_number":"","name":"","status_code":null,"enrollment_status_code":null,"on_campus_flag":null,"department":null,"campus_code":null,"campus_name":null,"major_code":null,"major_name":null,"grade":null,"class_code":null,"class_name":null,"student_category_code":null,"student_category_name":null,"study_years":null,"admission_month":null,"expected_graduation_date":null,"counselor":null,"email":null,"mobile":null,"source_updated_at":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) { assertPhase2JSON(t, tc.value, []byte(tc.want)) })
	}
	for _, input := range []string{`{}`, `{"is_active":null}`, `{"is_active":false}`, `{"is_active":true}`} {
		var request contract.OrganizationsRequest
		if err := json.Unmarshal([]byte(input), &request); err != nil {
			t.Fatal(err)
		}
		switch input {
		case `{}`, `{"is_active":null}`:
			if request.IsActive != nil {
				t.Fatalf("missing/null must remain unset: %s", input)
			}
		case `{"is_active":false}`:
			if request.IsActive == nil || *request.IsActive {
				t.Fatal("false filter lost")
			}
		case `{"is_active":true}`:
			if request.IsActive == nil || !*request.IsActive {
				t.Fatal("true filter lost")
			}
		}
	}
}

func TestT16_03OperationsUniqueAndNineRoutesRegistered(t *testing.T) {
	newOperations := map[contract.Operation]string{
		contract.Organizations:         "organizations.query",
		contract.Students:              "students.query",
		contract.Staff:                 "staff.query",
		contract.StaffTeachingClasses:  "staff.teaching-classes.query",
		contract.TeachingClassStudents: "teaching-classes.students.query",
	}
	if len(newOperations) != 5 {
		t.Fatal("new operations are not unique")
	}
	for operation, want := range newOperations {
		if string(operation) != want {
			t.Fatalf("operation changed: %s != %s", operation, want)
		}
	}
	want := []contract.Definition{
		{Operation: contract.StudentGrades, Path: "/api/v1/sicp/students/grades/query"},
		{Operation: contract.BatchGrades, Path: "/api/v1/sicp/students/grades/batch-query"},
		{Operation: contract.StudentTimetable, Path: "/api/v1/sicp/students/timetable/query"},
		{Operation: contract.StaffTimetable, Path: "/api/v1/sicp/staff/timetable/query"},
		{Operation: contract.Organizations, Path: "/api/v1/sicp/organizations/query"},
		{Operation: contract.Students, Path: "/api/v1/sicp/students/query"},
		{Operation: contract.Staff, Path: "/api/v1/sicp/staff/query"},
		{Operation: contract.StaffTeachingClasses, Path: "/api/v1/sicp/staff/teaching-classes/query"},
		{Operation: contract.TeachingClassStudents, Path: "/api/v1/sicp/teaching-classes/students/query"},
	}
	if !reflect.DeepEqual(contract.Definitions(), want) {
		t.Fatal("production catalog must contain exactly the nine implemented operations")
	}
	for _, definition := range contract.LegacyBusinessDefinitions() {
		if _, exists := newOperations[definition.Operation]; exists {
			t.Fatal("new operation overlaps a legacy operation")
		}
	}
}
