package operations

import (
	"sort"
	"strings"
	"testing"
)

// The expected request fields are reviewed against the public contract and
// parser, not synthesized from the reflection metadata being checked.
func TestT32_01And02ReviewedBusinessRequestFields(t *testing.T) {
	grades := "semester course_code course_name course_attribute_codes course_nature_codes course_type_codes course_department_code course_department_name score_min score_max page page_size"
	for _, tc := range []struct {
		id, required, optional string
		pageSize               int
	}{
		{"students.grades.query", "student_number", "academic_year " + grades, 100},
		{"students.grades.batch-query", "academic_year", "student_numbers student_department_code student_department_name class_code class_name " + grades, 100},
		{"students.timetable.query", "student_number academic_year semester", "course_query", 0},
		{"staff.timetable.query", "staff_number academic_year semester", "course_query teaching_class_number", 0},
		{"organizations.query", "", "page page_size organization_code organization_name parent_organization_code category_codes is_active", 1000},
		{"students.query", "", "page page_size student_number name department_code department_name campus_code major_code grade class_code status_codes enrollment_status_codes on_campus_flag student_category_codes study_years admission_year expected_graduation_year counselor_staff_number", 1000},
		{"staff.query", "", "page page_size staff_number name department_code department_name status_codes", 1000},
		{"staff.teaching-classes.query", "staff_number academic_year semester", "page page_size course_code", 100},
		{"teaching-classes.students.query", "teaching_class_number planned_course_code academic_year semester", "page page_size", 100},
	} {
		t.Run(tc.id, func(t *testing.T) {
			api, ok := LookupAPI(tc.id)
			if !ok || api.Method != "POST" || api.Kind != "business" {
				t.Fatal("missing business definition")
			}
			want := map[string]bool{}
			for _, name := range strings.Fields(tc.required) {
				want[name] = true
			}
			for _, name := range strings.Fields(tc.optional) {
				want[name] = false
			}
			if len(want) != len(api.Request.Properties) {
				t.Fatal("field set changed", api.Request.Properties)
			}
			for name, required := range want {
				f, ok := api.Request.Properties[name]
				if !ok || f.Required != required || f.Nullable == required {
					t.Fatal("presence/null contract", name, f)
				}
				wantType := "string"
				switch {
				case name == "is_active":
					wantType = "boolean"
				case name == "page" || name == "page_size" || name == "study_years":
					wantType = "integer"
				case name == "score_min" || name == "score_max":
					wantType = "number"
				case strings.HasSuffix(name, "_codes") || name == "student_numbers":
					wantType = "array"
				}
				if f.Type != wantType {
					t.Fatal("type mismatch", name, f.Type)
				}
				if f.Type == "array" && (f.Items == nil || f.Items.Nullable || f.Items.Type != "string" || !strings.Contains(f.Items.Limit, "512")) {
					t.Fatal("array item contract", name)
				}
			}
			if api.Snapshot != (tc.pageSize > 0) {
				t.Fatal("snapshot applicability")
			}
			if tc.pageSize > 0 && (api.Request.Properties["page"].Default != 1 || api.Request.Properties["page_size"].Default != tc.pageSize || api.Request.Properties["page_size"].Limit != "1..1000") {
				t.Fatal("pagination contract")
			}
			items := api.Response.Properties["items"]
			if items.Type != "array" || items.Nullable || items.Items == nil || items.Items.Type != "object" {
				t.Fatal("response array")
			}
		})
	}
}

func TestT32_01And03CatalogIdentityAndSourceExecutionBoundary(t *testing.T) {
	all := Catalog()
	ids := map[string]bool{}
	sources, business := 0, 0
	for _, api := range all {
		if ids[api.ID] || api.ID == "" || api.Version != CatalogVersion || api.Path == "" || api.Readiness != "unknown" {
			t.Fatal("invalid catalog identity", api.ID)
		}
		ids[api.ID] = true
		if api.Kind == "source" {
			sources++
			if api.Method != "GET" || api.Snapshot || len(api.Request.Properties) != 2 {
				t.Fatal("source execution contract")
			}
			r := TestRequest{APIID: api.ID, Version: CatalogVersion}
			if r.Normalize() != nil || r.Page != 1 || r.PageSize != 10 {
				t.Fatal("source defaults")
			}
			r.PageSize = 101
			if r.Normalize() != ErrArgument {
				t.Fatal("source accepted oversized page")
			}
			r.PageSize = 10
			r.Version = "obsolete"
			if r.Normalize() != ErrArgument {
				t.Fatal("old catalog executed")
			}
		} else {
			business++
			r := TestRequest{APIID: api.ID, Version: CatalogVersion}
			if r.Normalize() != ErrArgument {
				t.Fatal("business routed through source queue")
			}
		}
	}
	if sources != 14 || business != 9 || len(ids) != 23 || !sort.SliceIsSorted(all, func(i, j int) bool { return all[i].ID < all[j].ID }) {
		t.Fatal("catalog changed")
	}
}
