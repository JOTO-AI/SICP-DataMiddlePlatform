// Package querymock verifies public query responses against the repository's
// existing source-derived, fictional snapshots. It does not rewrite source data
// or derive expected responses using the production assemblers.
package querymock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

func Load(t *testing.T, root string) map[string][]json.RawMessage {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "mock_data", "snapshots", "initial", "dwd_*.json"))
	if err != nil || len(files) != 14 {
		t.Fatal("fourteen original source mock tables required", err)
	}
	tables := make(map[string][]json.RawMessage, len(files))
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil || len(rows) == 0 {
			t.Fatal("invalid or empty original mock", file, err)
		}
		tables[strings.TrimSuffix(filepath.Base(file), ".json")] = rows
	}
	return tables
}

// Query must invoke a real query handler and fail the test on non-200 status.
type Query func(contract.Operation, string, string) (map[string]any, string)

func items(t *testing.T, data map[string]any, total int) []any {
	t.Helper()
	values, ok := data["items"].([]any)
	stamp, isString := data["synced_at"].(string)
	_, err := time.Parse(time.RFC3339Nano, stamp)
	if data["total"] != float64(total) || !ok || !isString || err != nil {
		t.Fatalf("invalid data envelope: %#v", data)
	}
	return values
}

// Assert the operation-specific echoed identity and the complete data shape.
func echo(t *testing.T, data map[string]any, op contract.Operation, body string) {
	t.Helper()
	fields := []string{}
	switch op {
	case contract.StaffTeachingClasses:
		fields = []string{"staff_number", "academic_year", "semester"}
	case contract.TeachingClassStudents:
		fields = []string{"teaching_class_number", "planned_course_code", "academic_year", "semester"}
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatal(err)
	}
	if len(data) != 3+len(fields) {
		t.Fatal("unexpected response fields", data)
	}
	for _, field := range fields {
		if request[field] == nil || data[field] != request[field] {
			t.Fatal("wrong identity echo", field, data)
		}
	}
}

func exact(t *testing.T, q Query, op contract.Operation, body, expected string) {
	t.Helper()
	var want []any
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	data, token := q(op, body, "")
	echo(t, data, op, body)
	if token == "" || !reflect.DeepEqual(items(t, data, len(want)), want) {
		t.Fatalf("%s %s: got %#v, want %#v", op, body, data, want)
	}
	// Read the same published snapshot through the hot-cache path as well.
	hot, next := q(op, body, token)
	echo(t, hot, op, body)
	if next != token || !reflect.DeepEqual(items(t, hot, len(want)), want) {
		t.Fatal("cached snapshot differs", hot)
	}
}

func pages(t *testing.T, q Query, op contract.Operation, body, field string, expected []string) {
	t.Helper()
	var request map[string]any
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatal(err)
	}
	const size = 2
	token := ""
	got := []string{}
	// Include one page beyond the end to check stable total and an empty array.
	for page := 1; page <= (len(expected)+size-1)/size+1; page++ {
		request["page"], request["page_size"] = page, size
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		data, next := q(op, string(raw), token)
		echo(t, data, op, string(raw))
		if next == "" || token != "" && next != token {
			t.Fatal("snapshot changed")
		}
		token = next
		values := items(t, data, len(expected))
		remaining := len(expected) - (page-1)*size
		want := max(0, min(size, remaining))
		if len(values) != want {
			t.Fatal("incorrect page length", page, data)
		}
		for _, value := range values {
			row, ok := value.(map[string]any)
			if !ok {
				t.Fatal("invalid item", value)
			}
			id, ok := row[field].(string)
			if !ok {
				t.Fatal("missing identity", row)
			}
			got = append(got, id)
		}
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatal("missing, reordered or duplicate identities", got, expected)
	}
}

const studentOne = `[{"student_number":"00202600001","name":"虚构学生001","status_code":"01","enrollment_status_code":"1","on_campus_flag":"1","department":{"organization_code":"00D1","organization_name":"虚构单位1","parent_organization_code":"10001","category_code":"1","category_name":"教学院系","is_active":true},"campus_code":"01","campus_name":"虚构校区1","major_code":"00M1","major_name":"虚构专业1","grade":"2026","class_code":"00B01","class_name":"虚构班级1","student_category_code":"41","student_category_name":"专科生","study_years":3,"admission_month":"2026-09","expected_graduation_date":"2029-06-30","counselor":{"staff_number":"001","name":"虚构教师001"},"email":" student001@campus.example ","mobile":"00000000001","source_updated_at":"2026-08-05T07:50:00+08:00"}]`

func Students(t *testing.T, q Query) {
	t.Helper()
	exact(t, q, contract.Students, `{"student_number":"00202600001"}`, studentOne)
	exact(t, q, contract.Students, `{"student_number":"00202600001","admission_year":"2026","expected_graduation_year":"2029","study_years":3,"campus_code":"01","major_code":"00M1","class_code":"00B01","department_code":"00D1","counselor_staff_number":"001","status_codes":["01"],"enrollment_status_codes":["1"],"on_campus_flag":"1","student_category_codes":["41"]}`, studentOne)
	exact(t, q, contract.Students, `{"student_number":"00202600001","admission_year":"2025"}`, `[]`)
	ids := []string{}
	for i := 1; i <= 12; i++ {
		ids = append(ids, fmt.Sprintf("002026%05d", i))
	}
	pages(t, q, contract.Students, `{}`, "student_number", ids)
	pages(t, q, contract.Students, `{"class_code":"00B01"}`, "student_number", []string{"00202600001", "00202600005", "00202600009"})
}
