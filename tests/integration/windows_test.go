//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestI04_12(t *testing.T) {
	f := newRuntime(t, false)
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	var changedAt time.Time
	var mutationError error
	// Mutate after the grade page has already been encoded, while the same
	// full acquisition still has the remaining source categories to fetch.
	f.source.afterPage = func(table string, page int) {
		if table != "dwd_bzkscjzlb" || page != 1 {
			return
		}
		once.Do(func() {
			changedAt = time.Now().UTC().Truncate(time.Microsecond)
			var row map[string]json.RawMessage
			if mutationError = json.Unmarshal(f.source.rows[table][1], &row); mutationError != nil {
				return
			}
			row["KCCJ"] = json.RawMessage("91")
			row["ETL_TIME"], mutationError = json.Marshal(changedAt.In(zone).Format("2006-01-02 15:04:05.999999"))
			if mutationError != nil {
				return
			}
			f.source.rows[table][1], mutationError = json.Marshal(row)
		})
	}
	f.run(t, "initialize", "init")
	// Synchronize with the HTTP callback before reading its captured evidence.
	f.source.mu.Lock()
	observedAt, observedError := changedAt, mutationError
	f.source.mu.Unlock()
	initial := loadTask(t, f, "init")
	if observedError != nil || observedAt.IsZero() || observedAt.Before(*initial.StartedAt) || !observedAt.Before(*initial.PublishedAt) {
		t.Fatal("change was not inside full acquisition", observedError)
	}
	data, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
	if !reflect.DeepEqual(data["items"].([]any)[1], expectedJSON(t, "grade_g02.json")) {
		t.Fatal("full acquisition unexpectedly included post-page change")
	}
	f.run(t, "incremental", "catchup")
	next := loadTask(t, f, "catchup")
	data, _ = f.queryOK(t, contract.StudentGrades, allGrades, "")
	want := expectedJSON(t, "grade_g02.json").(map[string]any)
	want["score"] = float64(91)
	want["source_updated_at"] = observedAt.In(zone).Format(time.RFC3339Nano)
	if !reflect.DeepEqual(data["items"].([]any)[1], want) {
		t.Fatal("overlap did not capture in-flight full-acquisition change")
	}
	f.source.mu.Lock()
	requests := append([]sourceRequest(nil), f.source.audit...)
	f.source.mu.Unlock()
	count := 0
	expectedStart := initial.StartedAt.Add(-control.IncrementalOverlap).In(zone).Format("2006-01-02 15:04:05.999999")
	wrongStart := initial.PublishedAt.Add(-control.IncrementalOverlap).In(zone).Format("2006-01-02 15:04:05.999999")
	for _, request := range requests {
		if request.Start == "" {
			continue
		}
		count++
		if request.Start != expectedStart || request.Start == wrongStart || request.End != next.StartedAt.In(zone).Format("2006-01-02 15:04:05.999999") {
			t.Fatal("incremental request used publication time instead of full started_at")
		}
		if request.Table == "dwd_bzkscjzlb" && request.Rows != 1 {
			t.Fatal("catchup did not fetch exactly the injected grade")
		}
	}
	if count != 14 {
		t.Fatal("incomplete catchup source windows")
	}
	assertProgress(t, f, "catchup")
}

func TestI04_13(t *testing.T) {
	f := newRuntime(t, false)
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	latest := time.Now().Add(-30 * time.Second).In(zone).Truncate(time.Microsecond)
	types := []struct{ table, key, field, want string }{
		{"dwd_bzksxjjbsjzlb", "XH", "YXSH", "D1"},
		{"dwd_bzksgrtxsjzlb", "XH", "YDDH", "000-Synthetic-Student-Latest"},
		{"dwd_grtxfszlb", "JGH", "YDDH", "000-Synthetic-Staff-Latest"},
	}
	for _, kind := range types {
		rows := make([]json.RawMessage, 1002)
		for i := range rows {
			value := fmt.Sprintf("Synthetic-Old-%04d", i)
			if i == len(rows)-1 {
				value = kind.want
			}
			row := map[string]string{kind.key: "001", kind.field: value, "ETL_TIME": latest.Add(time.Duration(i-1001) * time.Millisecond).Format("2006-01-02 15:04:05.999999")}
			if kind.table == "dwd_bzksxjjbsjzlb" {
				row["SZBH"] = "B1"
			}
			rows[i], err = json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
		}
		f.source.rows[kind.table] = rows
	}
	f.run(t, "initialize", "init")
	_, manifest := loadBundle(t, f, "Cinit")
	files, err := staging.NewStore("init", f.wr.Files.Staging, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := files.OpenSealed(f.e.ctx, manifest.Input)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range types {
		counts := map[int]int{}
		for _, page := range sealed.Pages() {
			if page.Request.Table == kind.table {
				if page.Total != 1002 {
					t.Fatal("pagination total confused source rows with current winners")
				}
				counts[page.Request.Page] = page.Summary.Count
			}
		}
		if !reflect.DeepEqual(counts, map[int]int{1: 1000, 2: 2}) {
			t.Fatalf("history source pages incomplete: %s %v", kind.table, counts)
		}
		found := false
		for _, table := range manifest.Tables {
			if table.Table == kind.table {
				found = true
				if table.Rows != 1 {
					t.Fatal("history did not collapse to one current record")
				}
			}
		}
		if !found {
			t.Fatal("history target missing")
		}
	}
	check := func() {
		for _, index := range []int{0, 2} {
			conn := f.e.connection(t, index)
			for _, kind := range types {
				var count int
				var value string
				if err := conn.QueryRow(f.e.ctx, "SELECT count(*) FROM source."+kind.table).Scan(&count); err != nil || count != 1 {
					t.Fatal("history primary identity not unique", err)
				}
				sql := fmt.Sprintf(`SELECT "%s" FROM source.%s WHERE "%s"='001'`, kind.field, kind.table, kind.key)
				if err := conn.QueryRow(f.e.ctx, sql).Scan(&value); err != nil || value != kind.want {
					t.Fatal("late history replaced current winner", err)
				}
			}
		}
		data, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
		for _, item := range data["items"].([]any) {
			student := item.(map[string]any)["student"].(map[string]any)
			if student["department_code"] != "D1" || student["department_name"] != "Mock Department" || student["class_code"] != "B1" {
				t.Fatal("current enrollment relation regressed")
			}
		}
	}
	check()
	f.source.mu.Lock()
	for _, kind := range types {
		row := map[string]string{kind.key: "001", kind.field: "Synthetic-Late-Old", "ETL_TIME": latest.Add(-2 * time.Second).Format("2006-01-02 15:04:05.999999")}
		if kind.table == "dwd_bzksxjjbsjzlb" {
			row["SZBH"] = "OLD"
		}
		raw, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			f.source.mu.Unlock()
			t.Fatal(marshalErr)
		}
		f.source.rows[kind.table] = []json.RawMessage{raw}
	}
	f.source.mu.Unlock()
	f.run(t, "incremental", "late")
	if task := loadTask(t, f, "late"); task.Status != "success_unchanged" {
		t.Fatal("late old history changed current combination")
	}
	check()
	f.source.mu.Lock()
	defer f.source.mu.Unlock()
	for _, kind := range types {
		found := false
		for _, request := range f.source.audit {
			if request.Start != "" && request.Table == kind.table && request.Rows == 1 {
				found = true
			}
		}
		if !found {
			t.Fatal("late row was filtered out rather than compared by priority time")
		}
	}
}
