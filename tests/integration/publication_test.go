//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func activeMetadata(t *testing.T, f *runtimeFixture) control.ActiveMetadata {
	t.Helper()
	a, exists, err := f.wr.Active.Read(f.e.ctx)
	if err != nil || !exists {
		t.Fatalf("active metadata missing: %v", err)
	}
	return a
}
func loadTask(t *testing.T, f *runtimeFixture, id string) control.Task {
	t.Helper()
	task := control.Task{ID: id}
	if err := f.wr.Control.Load(f.e.ctx, &task); err != nil {
		t.Fatal(err)
	}
	return task
}
func loadBundle(t *testing.T, f *runtimeFixture, id string) (control.Bundle, candidate.Manifest) {
	t.Helper()
	b := control.Bundle{ID: id}
	if err := f.wr.Control.Load(f.e.ctx, &b); err != nil {
		t.Fatal(err)
	}
	var m candidate.Manifest
	if err := json.Unmarshal([]byte(b.Tables), &m); err != nil {
		t.Fatal(err)
	}
	return b, m
}
func countControl(t *testing.T, f *runtimeFixture, sql string, args ...any) int {
	t.Helper()
	var count int
	if err := f.e.connection(t, 4).QueryRow(f.e.ctx, sql, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func atWriteBarrier(t *testing.T, f *runtimeFixture, target storage.Position, mode, id string, inspect func()) {
	t.Helper()
	connection, err := f.wc.Databases.Business(target)
	if err != nil {
		t.Fatal(err)
	}
	original := f.wr.Database
	pause := &pausedDatabase{Postgres: original.(*adapters.Postgres), target: connection, entered: make(chan struct{}), resume: make(chan struct{})}
	f.wr.Database = pause
	admitTask(t, f, mode, id)
	ctx, cancel := context.WithCancel(f.e.ctx)
	done := make(chan error, 1)
	var once sync.Once
	release := func() { once.Do(func() { close(pause.resume) }) }
	finished := false
	t.Cleanup(func() {
		release()
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("write barrier cleanup timeout")
			}
		}
	})
	go func() { done <- f.worker.Engine.Cycle(ctx) }()
	select {
	case <-pause.entered:
	case err := <-done:
		finished = true
		t.Fatalf("worker before barrier: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("write barrier timeout")
	}
	inspect()
	release()
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("worker completion timeout")
	}
	cancel()
	f.wr.Database = original
	if task := loadTask(t, f, id); task.Status != "success_published" {
		t.Fatalf("task status: %s", task.Status)
	}
}
func assertProgress(t *testing.T, f *runtimeFixture, id string) {
	t.Helper()
	task := loadTask(t, f, id)
	if task.StartedAt == nil || task.CheckedAt == nil {
		t.Fatal("task times missing")
	}
	conn := f.e.connection(t, 4)
	rows, err := conn.Query(f.e.ctx, "SELECT source,confirmed_until,checked_at,task_id FROM control.source_progress ORDER BY source")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name, taskID string
		var until, checked time.Time
		if err := rows.Scan(&name, &until, &checked, &taskID); err != nil {
			t.Fatal(err)
		}
		if taskID != id || !until.Equal(*task.StartedAt) || !checked.Equal(*task.CheckedAt) {
			t.Fatalf("incorrect source progress: %s", name)
		}
		seen[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 14 {
		t.Fatal("incomplete source progress")
	}
	for _, table := range source.Catalog() {
		if !seen[table.Name] {
			t.Fatal("source progress missing", table.Name)
		}
	}
}
func assertUnpublished(t *testing.T, f *runtimeFixture) {
	t.Helper()
	if _, exists, err := f.wr.Active.Read(f.e.ctx); err != nil || exists {
		t.Fatal("partial initialization exposed active route", err)
	}
	for _, sql := range []string{"SELECT count(*) FROM control.source_progress", "SELECT count(*) FROM control.publication_outbox", "SELECT count(*) FROM control.slots WHERE admission_open", "SELECT count(*) FROM control.bundles WHERE published_at IS NOT NULL"} {
		if countControl(t, f, sql) != 0 {
			t.Fatal("partial initialization advanced publication/progress")
		}
	}
}
func TestI04_01(t *testing.T) {
	f := newRuntime(t, false)
	assertUnpublished(t, f)
	atWriteBarrier(t, f, storage.Position{Instance: storage.Standby, Slot: 0}, "initialize", "init", func() {
		assertUnpublished(t, f)
		if n := countControl(t, f, "SELECT count(*) FROM control.candidates WHERE state='ready'"); n != 1 {
			t.Fatalf("first-copy evidence missing: %d", n)
		}
		status, envelope, _ := f.queryJSON(t, contract.StudentGrades, allGrades, "", f.qc.Service.APIKey)
		if status < 400 || envelope["data"] != nil {
			t.Fatal("single candidate served queries")
		}
	})
	active := activeMetadata(t, f)
	_, manifest := loadBundle(t, f, active.Position.BundleID)
	if len(manifest.Tables) != 14 || countControl(t, f, "SELECT count(*) FROM control.candidates WHERE state='ready' AND evidence IS NOT NULL AND bundle_id=$1", active.Position.BundleID) != 2 || countControl(t, f, "SELECT count(*) FROM control.slots WHERE state='ready' AND admission_open AND bundle_id=$1", active.Position.BundleID) != 2 {
		t.Fatal("incomplete initial copies")
	}
	assertProgress(t, f, "init")
	f.queryOK(t, contract.StudentGrades, allGrades, "")
}
func TestI04_02(t *testing.T) {
	for _, table := range source.Catalog() {
		t.Run("source/"+table.Name, func(t *testing.T) {
			f := newRuntime(t, false)
			f.source.failTable = table.Name
			admitTask(t, f, "initialize", "init")
			if err := f.worker.Engine.Cycle(f.e.ctx); err == nil {
				t.Fatal("source failure accepted")
			}
			assertUnpublished(t, f)
			if countControl(t, f, "SELECT count(*) FROM control.candidates") != 0 {
				t.Fatal("partial acquisition started a build")
			}
		})
	}
	for _, index := range []int{0, 2} {
		t.Run(fmt.Sprintf("candidate/%d", index), func(t *testing.T) {
			f := newRuntime(t, false)
			conn := f.e.connection(t, index)
			t.Cleanup(func() {
				if _, err := conn.Exec(context.Background(), "GRANT SELECT ON source.dwd_bzkscjzlb TO sicp_query"); err != nil {
					t.Error(err)
				}
			})
			if _, err := conn.Exec(f.e.ctx, "REVOKE SELECT ON source.dwd_bzkscjzlb FROM sicp_query"); err != nil {
				t.Fatal(err)
			}
			admitTask(t, f, "initialize", "init")
			if err := f.worker.Engine.Cycle(f.e.ctx); err == nil {
				t.Fatal("unreadable candidate published")
			}
			assertUnpublished(t, f)
		})
	}
}
func TestI04_03(t *testing.T) {
	f := newRuntime(t, true)
	before := activeMetadata(t, f)
	ctx, cancel := context.WithCancel(f.e.ctx)
	defer cancel()
	type observation struct {
		values map[control.ActiveMetadata]int
		err    error
	}
	done := make(chan observation, 1)
	go func() {
		out := observation{values: map[control.ActiveMetadata]int{}}
		for ctx.Err() == nil {
			a, exists, err := f.wr.Active.Read(ctx)
			if err != nil {
				if ctx.Err() == nil {
					out.err = err
				}
				break
			}
			if !exists || a.Validate() != nil {
				out.err = fmt.Errorf("partial active metadata")
				break
			}
			out.values[a]++
			time.Sleep(time.Millisecond)
		}
		done <- out
	}()
	finished := false
	t.Cleanup(func() {
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("active reader cleanup timeout")
			}
		}
	})
	f.run(t, "force_full", "next")
	after := activeMetadata(t, f)
	cancel()
	out := <-done
	finished = true
	if out.err != nil {
		t.Fatal(out.err)
	}
	valid := map[control.ActiveMetadata]bool{before: true, after: true}
	rows, err := f.e.connection(t, 4).Query(f.e.ctx, "SELECT target::text FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		value, err := control.DecodeActive(control.Document(raw))
		if err != nil {
			t.Fatal(err)
		}
		valid[value] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(valid) != 3 || len(out.values) < 2 {
		t.Fatal("did not observe transition between complete metadata values")
	}
	for value := range out.values {
		if !valid[value] {
			t.Fatal("Redis exposed mixed metadata fields")
		}
	}
}
func TestI04_09(t *testing.T) {
	f := newRuntime(t, true)
	before := activeMetadata(t, f)
	bundle, _ := loadBundle(t, f, before.Position.BundleID)
	f.run(t, "incremental", "unchanged")
	after := activeMetadata(t, f)
	next, _ := loadBundle(t, f, after.Position.BundleID)
	task := loadTask(t, f, "unchanged")
	if before != after || task.Status != "success_unchanged" || !bundle.PublishedAt.Equal(*next.PublishedAt) || !next.CheckedAt.After(*bundle.CheckedAt) || countControl(t, f, "SELECT count(*) FROM control.publication_outbox") != 1 {
		t.Fatal("unchanged window republished or lost progress")
	}
	assertProgress(t, f, "unchanged")
	f.source.mu.Lock()
	defer f.source.mu.Unlock()
	windows := 0
	for _, request := range f.source.audit {
		if request.Start != "" {
			windows++
			if request.Rows != 0 {
				t.Fatal("unchanged fixture unexpectedly returned incremental rows")
			}
		}
	}
	if windows != 14 {
		t.Fatalf("incremental source windows: %d", windows)
	}
}
func TestI04_10(t *testing.T) {
	f := newRuntime(t, true)
	before := activeMetadata(t, f)
	oldBundle, old := loadBundle(t, f, before.Position.BundleID)
	f.run(t, "force_full", "full")
	after := activeMetadata(t, f)
	bundle, current := loadBundle(t, f, after.Position.BundleID)
	if before.Position.BundleID == after.Position.BundleID || !bundle.PublishedAt.After(*oldBundle.PublishedAt) || !reflect.DeepEqual(old.Tables, current.Tables) {
		t.Fatal("same-content full did not publish independent combination")
	}
	if countControl(t, f, "SELECT count(*) FROM control.candidates WHERE task_id='full' AND state='ready' AND evidence IS NOT NULL") != 2 {
		t.Fatal("full did not build both candidates")
	}
	for _, index := range []int{1, 3} {
		var count int
		if err := f.e.connection(t, index).QueryRow(f.e.ctx, "SELECT count(*) FROM source.dwd_bzkscjzlb").Scan(&count); err != nil || count != 2 {
			t.Fatalf("new physical copy %d: %d %v", index, count, err)
		}
	}
	assertProgress(t, f, "full")
}
func TestI04_11(t *testing.T) {
	for _, mode := range []string{"force_full", "incremental"} {
		t.Run(mode, func(t *testing.T) {
			f := newRuntime(t, true)
			f.source.mu.Lock()
			f.source.rows["dwd_bzkscjzlb"] = f.source.rows["dwd_bzkscjzlb"][1:]
			f.source.mu.Unlock()
			f.run(t, mode, "delete")
			data, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
			want := 2
			if mode == "force_full" {
				want = 1
			}
			items := data["items"].([]any)
			if data["total"] != float64(want) || len(items) != want {
				t.Fatal("deletion semantics violated")
			}
			if !reflect.DeepEqual(items[len(items)-1], expectedJSON(t, "grade_g02.json")) {
				t.Fatal("retained row changed")
			}
			if mode == "incremental" && items[0].(map[string]any)["grade_id"] != "G01" {
				t.Fatal("incremental removed absent row")
			}
		})
	}
}
