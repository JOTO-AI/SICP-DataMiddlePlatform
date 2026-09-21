package contract_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

// Fixtures are independent, synthetic full-field examples transcribed from
// sicp-api-design.md. Exact JSON comparison detects missing and extra fields.
func TestT01_02FullDTOFieldAndValueRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target any
	}{
		{"student_grades_request", &contract.StudentGradesRequest{}},
		{"batch_grades_request", &contract.BatchGradesRequest{}},
		{"student_timetable_request", &contract.StudentTimetableRequest{}},
		{"staff_timetable_request", &contract.StaffTimetableRequest{}},
		{"grades_response", &contract.GradesResponse{}},
		{"student_timetable_response", &contract.StudentTimetableResponse{}},
		{"staff_timetable_response", &contract.StaffTimetableResponse{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(data, tc.target); err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(tc.target)
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			if err = json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("contract fields or values changed\nwant: %s\ngot: %s", data, out)
			}
		})
	}
}

func TestResponseArraysNeverSerializeNull(t *testing.T) {
	for _, v := range []any{contract.GradesResponse{}, contract.StudentTimetableResponse{}, contract.StaffTimetableResponse{}} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err = json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if string(m["items"]) != "[]" {
			t.Fatal(string(data))
		}
	}
	data, err := json.Marshal(contract.TimetableEntry{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"teacher_staff_numbers", "teacher_names", "sections", "weeks", "dates"} {
		if string(m[key]) != "[]" {
			t.Fatal(key, string(data))
		}
	}
	if string(m["enrolled_count"]) != "null" || string(m["credits"]) != "null" || string(m["weekday"]) != "0" {
		t.Fatal(string(data))
	}
}
