package operations

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

const CatalogVersion = "2026-10-13.v1"

type Field struct {
	Type       string           `json:"type"`
	Required   bool             `json:"required"`
	Nullable   bool             `json:"nullable"`
	Default    any              `json:"default"`
	Limit      string           `json:"limit"`
	Properties map[string]Field `json:"properties,omitempty"`
	Items      *Field           `json:"items,omitempty"`
}
type API struct {
	ID             string   `json:"id"`
	Version        string   `json:"version"`
	Purpose        string   `json:"purpose"`
	Method         string   `json:"method"`
	Path           string   `json:"path"`
	Kind           string   `json:"kind"`
	Implemented    bool     `json:"implemented"`
	Readiness      string   `json:"readiness"`
	Snapshot       bool     `json:"snapshot"`
	Request        Field    `json:"request"`
	RequestExample any      `json:"request_example"`
	Response       Field    `json:"response"`
	SuccessExample any      `json:"success_example"`
	FailureExample any      `json:"failure_example"`
	Notes          []string `json:"notes"`
}

func schema(t reflect.Type, request bool) Field {
	f := Field{Nullable: t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice, Required: request && t.Kind() != reflect.Pointer && t.Kind() != reflect.Slice, Limit: ""}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		f.Type = "object"
		f.Properties = map[string]Field{}
		for i := 0; i < t.NumField(); i++ {
			p := t.Field(i)
			if p.Anonymous {
				for k, v := range schema(p.Type, request).Properties {
					f.Properties[k] = v
				}
				continue
			}
			name := strings.Split(p.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			child := schema(p.Type, request)
			if request && name == "page" {
				child.Default = 1
				child.Limit = "1..1000000"
			}
			if request && name == "page_size" {
				child.Default = 100
				child.Limit = "1..1000"
			}
			if request && name == "academic_year" {
				child.Limit = "YYYY-YYYY; consecutive years"
			}
			if request && name == "semester" {
				child.Limit = "1 or 2"
			}
			if request && (name == "admission_year" || name == "expected_graduation_year") {
				child.Limit = "0001..9999; four digits"
			}
			if request && name == "study_years" {
				child.Limit = "1..2147483647"
			}
			if request && (name == "score_min" || name == "score_max") {
				child.Limit = "finite number; score_min <= score_max"
			}
			f.Properties[name] = child
		}
	case reflect.String:
		f.Type = "string"
		if request {
			f.Limit = "nonblank; <=512 UTF-8 bytes"
		}
	case reflect.Bool:
		f.Type = "boolean"
	case reflect.Int, reflect.Int32, reflect.Int64:
		f.Type = "integer"
	case reflect.Float32, reflect.Float64:
		f.Type = "number"
	case reflect.Slice, reflect.Array:
		f.Type = "array"
		child := schema(t.Elem(), false)
		f.Items = &child
		if request {
			f.Limit = "1..1000 nonblank values when supplied"
		} else {
			f.Nullable = false
		}
	default:
		f.Type = "json"
	}
	return f
}
func Catalog() []API {
	out := []API{}
	types := map[contract.Operation][2]any{
		contract.StudentGrades: {contract.StudentGradesRequest{}, contract.GradesResponse{}}, contract.BatchGrades: {contract.BatchGradesRequest{}, contract.GradesResponse{}}, contract.StudentTimetable: {contract.StudentTimetableRequest{}, contract.StudentTimetableResponse{}}, contract.StaffTimetable: {contract.StaffTimetableRequest{}, contract.StaffTimetableResponse{}},
		contract.Organizations: {contract.OrganizationsRequest{}, contract.OrganizationsResponse{}}, contract.Students: {contract.StudentsRequest{}, contract.StudentsResponse{}}, contract.Staff: {contract.StaffRequest{}, contract.StaffResponse{}}, contract.StaffTeachingClasses: {contract.StaffTeachingClassesRequest{}, contract.StaffTeachingClassesResponse{}}, contract.TeachingClassStudents: {contract.TeachingClassStudentsRequest{}, contract.TeachingClassStudentsResponse{}},
	}
	for _, d := range contract.Definitions() {
		v := types[d.Operation]
		request := schema(reflect.TypeOf(v[0]), true)
		if d.Operation == contract.StudentGrades || d.Operation == contract.BatchGrades {
			field := request.Properties["academic_year"]
			field.Limit = "YYYY-YYYY with consecutive years, or all"
			request.Properties["academic_year"] = field
		}
		example := map[string]any{}
		for name, field := range request.Properties {
			if field.Required {
				example[name] = "synthetic-001"
				if name == "academic_year" {
					example[name] = "2025-2026"
				}
				if name == "semester" {
					example[name] = "1"
				}
			}
		}
		if d.Operation == contract.Organizations || d.Operation == contract.Students || d.Operation == contract.Staff {
			f := request.Properties["page_size"]
			f.Default = 1000
			request.Properties["page_size"] = f
		}
		notes := []string{"Business execution uses the existing /console-api/query proxy.", "Unknown fields are rejected; null optional request fields are treated as omitted.", "Response timestamps follow the existing business contract and are not log timestamps."}
		if contract.SupportsSnapshot(d.Operation) {
			notes = append(notes, "Use X-Query-Snapshot for subsequent pages; teaching-classes students must reuse the staff teaching-classes snapshot.")
		} else {
			notes = append(notes, "Timetable is not paginated and does not accept X-Query-Snapshot.")
		}
		data := map[string]any{}
		raw, _ := json.Marshal(v[1])
		_ = json.Unmarshal(raw, &data)
		data["synced_at"] = "2026-10-13T08:00:00+08:00"
		for name, value := range example {
			if _, exists := data[name]; exists {
				data[name] = value
			}
		}
		out = append(out, API{ID: string(d.Operation), Version: CatalogVersion, Purpose: string(d.Operation), Method: "POST", Path: d.Path, Kind: "business", Implemented: true, Readiness: "unknown", Snapshot: contract.SupportsSnapshot(d.Operation), Request: request, RequestExample: example, Response: schema(reflect.TypeOf(v[1]), false), SuccessExample: map[string]any{"code": "ok", "message": "OK", "request_id": "synthetic-request-1", "data": data}, FailureExample: map[string]any{"code": "invalid_argument", "message": "Invalid request", "request_id": "synthetic-request-1", "data": nil}, Notes: notes})
	}
	for _, d := range ingest.Catalog() {
		fields := map[string]Field{}
		for _, column := range d.Columns {
			typeName, limit := "string", "missing and null are distinct; source text preserved"
			if column.Kind == value.JSON {
				typeName, limit = "json", "any JSON token retained without numeric conversion"
			}
			if column.Kind == value.Timestamp {
				limit = "YYYY-MM-DD[T or space]HH:MM:SS with up to 6 fractional digits; no timezone"
			}
			if column.Name == d.PrimaryKey {
				limit = "required nonblank string; identifiers preserved"
			}
			fields[column.Name] = Field{Type: typeName, Nullable: column.Name != d.PrimaryKey, Required: column.Name == d.PrimaryKey, Limit: limit}
		}
		out = append(out, API{ID: d.Name, Version: CatalogVersion, Purpose: "Read one page of " + d.Name, Method: "GET", Path: d.Path, Kind: "source", Implemented: true, Readiness: "unknown", Request: Field{Type: "object", Properties: map[string]Field{"page": {Type: "integer", Default: 1, Limit: "1..1000000"}, "page_size": {Type: "integer", Default: 10, Limit: "1..100"}}}, RequestExample: map[string]any{"api_id": d.Name, "version": CatalogVersion, "page": 1, "page_size": 10, "idempotency_key": "synthetic-test-001"}, Response: Field{Type: "object", Properties: fields}, SuccessExample: map[string]any{"code": 200, "data": map[string]any{"total": 0, "list": []any{}}}, FailureExample: map[string]any{"code": 500, "message": "synthetic upstream failure"}, Notes: []string{"Catalog source fields describe each record.", "Test credentials and destination are fixed by deployment; at most one page, 2 MiB and 30 seconds including queue time.", "Test requests never advance synchronization progress; raw results expire within ten minutes."}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func LookupAPI(id string) (API, bool) {
	for _, d := range Catalog() {
		if d.ID == id {
			return d, true
		}
	}
	return API{}, false
}
