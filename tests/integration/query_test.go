//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

const allGrades = `{"student_number":"001","academic_year":"all","page":1,"page_size":100}`

func expectedJSON(t *testing.T, name string) any {
	t.Helper()
	raw, err := os.ReadFile("../fixtures/phase1/query/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	return expected
}
func TestI03_01(t *testing.T) {
	f := newRuntime(t, true)
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{
		{contract.StudentGrades, `{"student_number":"001","academic_year":"all","score_min":80,"page":1,"page_size":1}`},
		{contract.BatchGrades, `{"student_numbers":["001"],"academic_year":"2026-2027","score_min":80,"page":1,"page_size":1}`},
	} {
		data, token := f.queryOK(t, tc.op, tc.body, "")
		items, ok := data["items"].([]any)
		if len(data) != 3 || data["total"] != float64(1) || !ok || len(items) != 1 || token == "" || data["synced_at"] == "" {
			t.Fatalf("invalid grade envelope: %v", data)
		}
		if !reflect.DeepEqual(items[0], expectedJSON(t, "grade_g02.json")) {
			actual, _ := json.Marshal(items[0])
			t.Fatalf("grade fields differ: %s", actual)
		}
	}
}
func TestI03_02(t *testing.T) {
	f := newRuntime(t, false)
	// Keep a nonempty course table but remove the grades' optional relation.
	f.source.rows["dwd_bzkskcsjzlb"] = []json.RawMessage{json.RawMessage(`{"KCH":"UNRELATED","KCMC":"Other","ETL_TIME":"2026-08-05 07:50:00"}`)}
	f.run(t, "initialize", "init")
	data, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
	items := data["items"].([]any)
	if data["total"] != float64(2) || len(items) != 2 {
		t.Fatal("missing relation dropped primary grades")
	}
	for _, item := range items {
		course := item.(map[string]any)["course"].(map[string]any)
		if course["course_code"] != "C1" || course["course_name"] != nil || course["department"] != nil {
			t.Fatalf("missing course mapping: %v", course)
		}
	}
}
func TestI03_03(t *testing.T) {
	f := newRuntime(t, true)
	active, exists, err := f.wr.Active.Read(f.e.ctx)
	if err != nil || !exists {
		t.Fatalf("active route: %v", err)
	}
	index := active.Position.Position.Slot
	if active.Position.Position.Instance == storage.Standby {
		index += 2
	}
	conn := f.e.connection(t, index)
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), "ALTER TABLE source.integration_hidden_grades RENAME TO dwd_bzkscjzlb"); err != nil {
			t.Error(err)
		}
	})
	if _, err := conn.Exec(f.e.ctx, "ALTER TABLE source.dwd_bzkscjzlb RENAME TO integration_hidden_grades"); err != nil {
		t.Fatal(err)
	}
	status, envelope, _ := f.queryJSON(t, contract.StudentGrades, allGrades, "", f.qc.Service.APIKey)
	if status < 500 || envelope["code"] == "ok" || envelope["data"] != nil {
		t.Fatalf("SQL failure became success: status=%d code=%v", status, envelope["code"])
	}
}
func TestI03_04(t *testing.T) {
	f := newRuntime(t, true)
	cold, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
	var before int
	if err := f.e.connection(t, 4).QueryRow(f.e.ctx, "SELECT count(*) FROM control.readers").Scan(&before); err != nil || before == 0 {
		t.Fatalf("no SQL read evidence: %v", err)
	}
	hot, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
	var after int
	if err := f.e.connection(t, 4).QueryRow(f.e.ctx, "SELECT count(*) FROM control.readers").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cold, hot) || before != after {
		t.Fatal("cache hit changed fields or started new SQL")
	}
	status, body, _ := f.queryJSON(t, contract.StudentGrades, allGrades, "", "wrong-synthetic-key")
	if status != 401 || body["data"] != nil {
		t.Fatal("hot cache bypassed authentication")
	}
}
func TestI03_05(t *testing.T) {
	f := newRuntime(t, true)
	body := `{"student_number":"001","academic_year":"all","page":1,"page_size":1}`
	first, token := f.queryOK(t, contract.StudentGrades, body, "")
	second, retained := f.queryOK(t, contract.StudentGrades, strings.Replace(body, `"page":1`, `"page":2`, 1), token)
	if token == "" || token != retained || first["total"] != float64(2) || second["total"] != float64(2) {
		t.Fatal("pagination did not retain snapshot")
	}
	for i, data := range []map[string]any{first, second} {
		items := data["items"].([]any)
		want := []string{"G01", "G02"}[i]
		if len(items) != 1 || items[0].(map[string]any)["grade_id"] != want {
			t.Fatalf("page %d omission/order/duplicate: %v", i+1, items)
		}
	}
}
func TestI03_12(t *testing.T) {
	f := newRuntime(t, true)
	for _, tc := range []struct {
		op       contract.Operation
		identity string
	}{{contract.StudentTimetable, "student_number"}, {contract.StaffTimetable, "staff_number"}} {
		body := `{"` + tc.identity + `":"001","academic_year":"2026-2027","semester":"1"}`
		data, token := f.queryOK(t, tc.op, body, "")
		if len(data) != 5 || data[tc.identity] != "001" || data["academic_year"] != "2026-2027" || data["semester"] != "1" || data["synced_at"] == "" || token != "" {
			t.Fatalf("timetable response contract: %v", data)
		}
		if !reflect.DeepEqual(data["items"], expectedJSON(t, "timetable_items.json")) {
			actual, _ := json.Marshal(data["items"])
			t.Fatalf("timetable fields differ: %s", actual)
		}
	}
}
