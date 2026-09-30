//go:build integration && linux

package integration

import (
	"fmt"
	"testing"
)

func TestI06_FailedFullUpdateCanRepairTablesIndividuallyBeforePublication(t *testing.T) {
	d := newDeployment(t, true)
	idle := func(id string) {
		d.waitSQL(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND owner_id IS NULL", id), "1")
	}
	idle(d.initial)
	active, progress := d.active(t), d.progress(t)
	d.mockPatch(t, `{"snapshot":"updated","unknown_table":"dwd_bzksbjsjzlb","fail_table":"dwd_bzkscjzlb","fail_remaining":100}`)
	failed := d.submit(t, "force-full", "full-repair-source")
	result := d.waitTask(t, failed, "failed")
	idle(failed)
	checkTables := func(result map[string]any, completed, reused int) {
		t.Helper()
		tables := result["acquisition"].(map[string]any)["tables"].([]any)
		done, copied := 0, 0
		for _, item := range tables {
			table := item.(map[string]any)
			if table["status"] == "collected" {
				done++
			}
			if table["reused"] == true {
				copied++
			}
		}
		if len(tables) != 14 || done != completed || copied != reused {
			t.Fatal("full acquisition stopped or refetched complete tables", done, copied)
		}
	}
	checkTables(result, 12, 0)
	if d.active(t) != active || d.progress(t) != progress {
		t.Fatal("failed full update published partial data")
	}
	// Block grades while repairing the other table. Its success must be retained,
	// not replaced by the old published grades to manufacture a complete update.
	d.mockPatch(t, `{"unknown_table":null}`)
	repair := func(table, key string) string {
		t.Helper()
		status, envelope := d.request(t, "POST", d.workerURL+"/admin/v1/tables/"+table+"/force-full", managementSession, key, "{}")
		if status != 202 {
			t.Fatal("table repair rejected", status, envelope)
		}
		return envelope["data"].(map[string]any)["task_id"].(string)
	}
	one := repair("dwd_bzksbjsjzlb", "full-repair-one")
	result = d.waitTask(t, one, "failed")
	idle(one)
	checkTables(result, 13, 12)
	if result["predecessor_id"] != failed || d.active(t) != active {
		t.Fatal("partial repair lost lineage or published")
	}
	d.compose(t, "restart", "worker")
	d.waitLive(t, d.workerURL)
	// If the first repaired table is fetched again, it now fails. Only grades
	// should hit the upstream; all thirteen completed tables must be copied.
	d.mockPatch(t, `{"unknown_table":"dwd_bzksbjsjzlb","fail_remaining":0}`)
	two := repair("dwd_bzkscjzlb", "full-repair-two")
	result = d.waitTask(t, two, "success_published")
	idle(two)
	checkTables(result, 14, 13)
	if result["predecessor_id"] != one || result["progress_confirmed"] != true || d.active(t).Position.BundleID != "C"+two {
		t.Fatal("complete repair did not auto-publish", result)
	}
	if d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.source_progress p JOIN control.tasks t ON t.id='%s' WHERE p.confirmed_until=t.started_at", d.initial)) != "14" {
		t.Fatal("repair advanced old tables to repair time")
	}
	// A normal incremental run can subsequently advance the conservative bounds.
	d.mockPatch(t, `{"unknown_table":null}`)
	sync := d.submit(t, "incremental", "after-full-repair")
	result = d.waitTask(t, sync, "success_unchanged")
	if result["checked_at"] == nil || result["progress_confirmed"] != true {
		t.Fatal("incremental catch-up missing", result)
	}
}
