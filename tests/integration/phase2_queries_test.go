//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const phase2Teacher = `{"staff_number":"T001","academic_year":"2026-2027","semester":"1","page_size":1,"page":1}`
const phase2Roster = `{"teaching_class_number":"J001","planned_course_code":"C001","academic_year":"2026-2027","semester":"1","page_size":1,"page":1}`

func phase2Runtime(t *testing.T, mutate func(*mockSource)) *runtimeFixture {
	t.Helper()
	f := newRuntime(t, false)
	f.source.mu.Lock()
	for _, table := range source.Catalog() {
		f.source.rows[table.Name] = []json.RawMessage{}
	}
	add := func(table, text string) {
		row := rawRow(text)
		row["ETL_TIME"] = json.RawMessage(`"2026-09-01 08:00:00"`)
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		f.source.rows[table] = append(f.source.rows[table], data)
	}
	add("dwd_yxsdwjbsjzlb", `{"DWH":"10001","DWMC":"Synthetic School","DWYXBSM":"1","SFSC":"0"}`)
	add("dwd_yxsdwjbsjzlb", `{"DWH":"10002","DWMC":"Department","LSDWH":"10001","DWLBM":"1","DWYXBSM":"1","SFSC":"0"}`)
	add("dwd_yxsdwjbsjzlb", `{"DWH":"10003","DWMC":"Inactive","LSDWH":"10001","DWYXBSM":"0","SFSC":"0"}`)
	add("dwd_bzksbjsjzlb", `{"BH":"B001","BJMC":"Administrative Class","FDYJGH":"T001"}`)
	add("dwd_xqjbsjzlb", `{"XQH":"X001","XQMC":"Campus"}`)
	add("dwd_bzkszyxxsjzlb", `{"ZYH":"M001","ZYMC":"Major"}`)
	add("dwd_bzkskcsjzlb", `{"KCH":"C001","KCMC":"Course"}`)
	for _, id := range []string{"S001", "S002", "S003"} {
		add("dwd_bzksjbsjzlb", `{"XH":"`+id+`","XM":"Synthetic Student"}`)
		add("dwd_bzksxjjbsjzlb", `{"XH":"`+id+`","YXSH":"10002","SSXQH":"X001","ZYH":"M001","SZBH":"B001","SZNJ":"2026","XSLBM":"41","XZ":"3","RXNY":"202609","YJBYRQ":"20290630"}`)
		add("dwd_bzksgrtxsjzlb", `{"XH":"`+id+`","YDDH":"synthetic;raw","DZXX":"invalid@example.invalid;raw"}`)
	}
	for _, id := range []string{"T001", "T002", "T003"} {
		add("dwd_jzgjcsjzlb", `{"JGH":"`+id+`","XM":"Synthetic Teacher","DWH":"10002","DQZTM":"unknown"}`)
		add("dwd_grtxfszlb", `{"JGH":"`+id+`","YDDH":"synthetic;raw","DZXX":"invalid@example.invalid;raw"}`)
	}
	for _, id := range []string{"J001", "J002", "J003"} {
		add("dwd_bzkspksjzlb", `{"PKH":"`+id+`a","JXBH":"`+id+`","JHKCH":"C001","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T001","JXBMC":"Teaching Class"}`)
		add("dwd_bzkspksjzlb", `{"PKH":"`+id+`b","JXBH":"`+id+`","JHKCH":"C001","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T002"}`)
		for _, student := range []string{"S001", "S002", "MISSING"} {
			add("dwd_bzkskbsjzlb", `{"ID":"`+id+student+`","JXBH":"`+id+`","JHKCH":"C001","XH":"`+student+`"}`)
		}
		add("dwd_bzkskbsjzlb", `{"ID":"`+id+`duplicate","JXBH":"`+id+`","JHKCH":"C001","XH":"S001"}`)
	}
	add("dwd_bzkspksjzlb", `{"PKH":"empty","JXBH":"EMPTY","JHKCH":"C001","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T_EMPTY"}`)
	if mutate != nil {
		mutate(f.source)
	}
	f.source.mu.Unlock()
	f.run(t, "initialize", "init")
	return f
}

func phase2Item(t *testing.T, data map[string]any, index int) map[string]any {
	t.Helper()
	items, ok := data["items"].([]any)
	if !ok || index >= len(items) {
		t.Fatal("missing item")
	}
	item, ok := items[index].(map[string]any)
	if !ok {
		t.Fatal("invalid item")
	}
	return item
}

func TestI07_Phase2RealHTTPAssociations(t *testing.T) {
	f := phase2Runtime(t, nil)
	org, _ := f.queryOK(t, contract.Organizations, `{"is_active":false}`, "")
	if org["total"] != float64(1) || phase2Item(t, org, 0)["organization_code"] != "10003" {
		t.Fatal("inactive organization omitted")
	}
	student, _ := f.queryOK(t, contract.Students, `{"student_number":"S001","study_years":3,"admission_year":"2026","expected_graduation_year":"2029","counselor_staff_number":"T001"}`, "")
	want := rawRow(`{"student_number":"S001","name":"Synthetic Student","status_code":null,"enrollment_status_code":null,"on_campus_flag":null,"department":{"organization_code":"10002","organization_name":"Department","parent_organization_code":"10001","category_code":"1","category_name":"教学院系","is_active":true},"campus_code":"X001","campus_name":"Campus","major_code":"M001","major_name":"Major","grade":"2026","class_code":"B001","class_name":"Administrative Class","student_category_code":"41","student_category_name":"专科生","study_years":3,"admission_month":"2026-09","expected_graduation_date":"2029-06-30","counselor":{"staff_number":"T001","name":"Synthetic Teacher"},"email":"invalid@example.invalid;raw","mobile":"synthetic;raw","source_updated_at":"2026-09-01T08:00:00+08:00"}`)
	raw, _ := json.Marshal(want)
	var expected map[string]any
	_ = json.Unmarshal(raw, &expected)
	if student["total"] != float64(1) || !reflect.DeepEqual(phase2Item(t, student, 0), expected) {
		t.Fatal("student full fields differ")
	}
	staff, _ := f.queryOK(t, contract.Staff, `{"staff_number":"T001","status_codes":["unknown"]}`, "")
	profile := phase2Item(t, staff, 0)
	if len(profile) != 7 || profile["email"] != "invalid@example.invalid;raw" || profile["mobile"] != "synthetic;raw" || !reflect.DeepEqual(profile["department"], expected["department"]) {
		t.Fatal("staff contact or department changed")
	}
	classes, _ := f.queryOK(t, contract.StaffTeachingClasses, phase2Teacher, "")
	class := phase2Item(t, classes, 0)
	if classes["total"] != float64(3) || class["student_count"] != float64(3) || len(class["teachers"].([]any)) != 2 || class["course_name"] != "Course" {
		t.Fatal("class aggregation")
	}
	roster, _ := f.queryOK(t, contract.TeachingClassStudents, phase2Roster, "")
	missing := phase2Item(t, roster, 0)
	if roster["total"] != float64(3) || len(missing) != 7 || missing["student_number"] != "MISSING" || missing["name"] != nil || missing["class_code"] != nil || missing["department_code"] != nil {
		t.Fatal("missing profile member dropped")
	}
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{
		{contract.StudentGrades, `{"student_number":"S001","academic_year":"all"}`},
		{contract.BatchGrades, `{"student_numbers":["S001"],"academic_year":"all"}`},
		{contract.StudentTimetable, `{"student_number":"NO_SUCH_STUDENT","academic_year":"2026-2027","semester":"1"}`},
		{contract.StaffTimetable, `{"staff_number":"NO_SUCH_TEACHER","academic_year":"2026-2027","semester":"1"}`},
	} {
		f.queryOK(t, tc.op, tc.body, "")
	}
}

func TestI08_Phase2PaginationAndAtomicClient(t *testing.T) {
	f := phase2Runtime(t, nil)
	for _, tc := range []struct {
		op          contract.Operation
		field, body string
	}{
		{contract.Organizations, "organization_code", `{"page":1,"page_size":1}`},
		{contract.Students, "student_number", `{"page":1,"page_size":1}`},
		{contract.Staff, "staff_number", `{"page":1,"page_size":1}`},
		{contract.StaffTeachingClasses, "teaching_class_number", phase2Teacher},
		{contract.TeachingClassStudents, "student_number", phase2Roster},
	} {
		seen := map[string]bool{}
		token := ""
		for _, page := range []string{"1", "2", "3"} {
			data, next := f.queryOK(t, tc.op, strings.Replace(tc.body, `"page":1`, `"page":`+page, 1), token)
			if token != "" && next != token {
				t.Fatal("snapshot changed")
			}
			token = next
			id := phase2Item(t, data, 0)[tc.field].(string)
			if data["total"] != float64(3) || seen[id] {
				t.Fatal("page missing or duplicate")
			}
			seen[id] = true
		}
	}
	output := filepath.Join(t.TempDir(), "complete.json")
	cmd := exec.CommandContext(f.e.ctx, "python3", "../../scripts/sync_teaching_classes.py", "--base-url", f.server.URL, "--staff-number", "T001", "--academic-year", "2026-2027", "--semester", "1", "--page-size", "1", "--output", output)
	// Replace, rather than duplicate, a possibly inherited key.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "QUERY_API_KEY=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "QUERY_API_KEY="+f.qc.Service.APIKey)
	if err := cmd.Run(); err != nil {
		t.Fatal("atomic synchronization client failed", err)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Combination         string
		Classes, Membership []json.RawMessage
	}
	if json.Unmarshal(raw, &result) != nil || result.Combination == "" || len(result.Classes) != 3 || len(result.Membership) != 3 {
		t.Fatal("incomplete sync output")
	}
	empty, _ := f.queryOK(t, contract.TeachingClassStudents, strings.Replace(phase2Roster, "J001", "EMPTY", 1), "")
	if empty["total"] != float64(0) || len(empty["items"].([]any)) != 0 {
		t.Fatal("existing empty combination")
	}
	status, body, _ := f.queryJSON(t, contract.TeachingClassStudents, strings.Replace(phase2Roster, "J001", "ABSENT", 1), "", f.qc.Service.APIKey)
	if status != 400 || body["data"] != nil {
		t.Fatal("absent combination is not an empty roster")
	}
}

func TestI09_Phase2CachePublicationAndOldSnapshots(t *testing.T) {
	f := phase2Runtime(t, nil)
	type entry struct {
		op          contract.Operation
		body, token string
		data        map[string]any
	}
	entries := []entry{{op: contract.Organizations, body: `{}`}, {op: contract.Students, body: `{}`}, {op: contract.Staff, body: `{}`}, {op: contract.StaffTeachingClasses, body: phase2Teacher}, {op: contract.TeachingClassStudents, body: phase2Roster}}
	for i := range entries {
		e := &entries[i]
		e.data, e.token = f.queryOK(t, e.op, e.body, "")
		before := countControl(t, f, "SELECT count(*) FROM control.readers")
		hot, _ := f.queryOK(t, e.op, e.body, "")
		if !reflect.DeepEqual(e.data, hot) || before != countControl(t, f, "SELECT count(*) FROM control.readers") {
			t.Fatal("hot result read SQL or changed data")
		}
	}
	before := activeMetadata(t, f)
	f.run(t, "incremental", "unchanged")
	if activeMetadata(t, f) != before {
		t.Fatal("unchanged check published")
	}
	for _, e := range entries {
		current, _ := f.queryOK(t, e.op, e.body, e.token)
		if current["synced_at"] == e.data["synced_at"] || !reflect.DeepEqual(current["items"], e.data["items"]) {
			t.Fatal("metadata refresh changed cached payload or did not advance")
		}
	}
	f.source.mu.Lock()
	for i, raw := range f.source.rows["dwd_jzgjcsjzlb"] {
		f.source.rows["dwd_jzgjcsjzlb"][i] = json.RawMessage(strings.Replace(string(raw), "Synthetic Teacher", "Changed Teacher", 1))
	}
	f.source.mu.Unlock()
	f.run(t, "force_full", "changed")
	if activeMetadata(t, f).Position.BundleID == before.Position.BundleID {
		t.Fatal("change not published")
	}
	// Delete only this test's result-cache keys, forcing old snapshots to read
	// their retained physical copy rather than passing from cached payloads.
	keys, err := f.redis.raw.Do(f.e.ctx, "KEYS", f.redis.prefix+"query:*")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys.([]any) {
		if _, err := f.redis.raw.Do(f.e.ctx, "DEL", key); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range entries {
		old, _ := f.queryOK(t, e.op, e.body, e.token)
		if !reflect.DeepEqual(old["items"], e.data["items"]) {
			t.Fatal("old snapshot mixed combinations")
		}
	}
	current, _ := f.queryOK(t, contract.Staff, `{}`, "")
	if phase2Item(t, current, 0)["name"] != "Changed Teacher" {
		t.Fatal("new bundle not visible")
	}
}

func TestI10_Phase2AuthenticationAndInvalidSources(t *testing.T) {
	f := phase2Runtime(t, nil)
	_, token := f.queryOK(t, contract.StaffTeachingClasses, phase2Teacher, "")
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{{contract.Organizations, `{}`}, {contract.Students, `{}`}, {contract.Staff, `{}`}, {contract.StaffTeachingClasses, phase2Teacher}, {contract.TeachingClassStudents, phase2Roster}} {
		status, body, next := f.queryJSON(t, tc.op, tc.body, "", "wrong-synthetic-key")
		if status != http.StatusUnauthorized || body["data"] != nil || next != "" {
			t.Fatal("unauthorized result")
		}
	}
	status, body, next := f.queryJSON(t, contract.TeachingClassStudents, phase2Roster, token, f.qc.Service.APIKey)
	if status != 409 || body["data"] != nil || next != "" {
		t.Fatal("cross operation snapshot")
	}
	// Expiry is checked before serving a hot cache entry.
	if _, err := f.e.connection(t, 4).Exec(f.e.ctx, "UPDATE control.query_snapshots SET expires_at=created_at+interval '1 microsecond' WHERE token=$1", token); err != nil {
		t.Fatal(err)
	}
	status, body, next = f.queryJSON(t, contract.StaffTeachingClasses, phase2Teacher, token, f.qc.Service.APIKey)
	if status != 409 || body["data"] != nil || next != "" {
		t.Fatal(snapshot.Header, "expired cache accepted")
	}
	for _, tc := range []struct {
		name    string
		op      contract.Operation
		request string
		status  int
		mutate  func(*mockSource)
	}{
		{"missing-enrollment", contract.Students, `{}`, 500, func(m *mockSource) { m.rows["dwd_bzksxjjbsjzlb"] = m.rows["dwd_bzksxjjbsjzlb"][1:] }},
		{"nonintegral-years", contract.Students, `{}`, 500, func(m *mockSource) {
			m.rows["dwd_bzksxjjbsjzlb"][0] = json.RawMessage(strings.Replace(string(m.rows["dwd_bzksxjjbsjzlb"][0]), `"XZ":"3"`, `"XZ":"3.5"`, 1))
		}},
		{"ambiguous-term", contract.TeachingClassStudents, phase2Roster, 409, func(m *mockSource) {
			m.rows["dwd_bzkspksjzlb"] = append(m.rows["dwd_bzkspksjzlb"], json.RawMessage(`{"PKH":"ambiguous","JXBH":"J001","JHKCH":"C001","KKXND":"2025-2026","KKXQM":"1","JSJGH":"T001","ETL_TIME":"2026-09-01 08:00:00"}`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := phase2Runtime(t, tc.mutate)
			status, body, next := bad.queryJSON(t, tc.op, tc.request, "", bad.qc.Service.APIKey)
			if status != tc.status || body["data"] != nil || next != "" {
				t.Fatal("invalid source returned partial success", status)
			}
		})
	}
}
