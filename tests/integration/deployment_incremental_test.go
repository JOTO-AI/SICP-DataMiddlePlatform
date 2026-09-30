//go:build integration && linux

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func TestI06_ManualIncrementalAutomaticallyPublishesOnlyVerifiedChanges(t *testing.T) {
	d := newDeployment(t, true)
	idle := func(id string) {
		d.waitSQL(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND owner_id IS NULL", id), "1")
	}
	idle(d.initial)
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Second).In(zone).Format("2006-01-02 15:04:05.000000")
	d.mockPatch(t, fmt.Sprintf(`{"snapshot":"updated","stamp":"%s"}`, stamp))
	id := d.submit(t, "incremental", "manual-incremental-changed")
	result := d.waitTask(t, id, "success_published")
	if result["mode"] != "incremental" || result["operator"] == "scheduler" || result["published_at"] == nil || result["progress_confirmed"] != true {
		t.Fatal("manual sync did not auto-publish", result)
	}
	active := d.assertFlowState(t, id, id, storage.Main, 88.5, 13, false)
	idle(id)
	unchanged := d.submit(t, "incremental", "manual-incremental-unchanged")
	result = d.waitTask(t, unchanged, "success_unchanged")
	if result["published_at"] != nil || result["progress_confirmed"] != true || d.active(t) != active {
		t.Fatal("unchanged sync republished or lost progress", result)
	}
	idle(unchanged)
	progress := d.progress(t)
	d.mockPatch(t, `{"unknown_table":"dwd_bzksbjsjzlb"}`)
	failed := d.submit(t, "incremental", "manual-incremental-failed")
	result = d.waitTask(t, failed, "failed")
	if result["published_at"] != nil || result["progress_confirmed"] != false || d.active(t) != active || d.progress(t) != progress {
		t.Fatal("failed sync published or advanced progress", result)
	}
}
