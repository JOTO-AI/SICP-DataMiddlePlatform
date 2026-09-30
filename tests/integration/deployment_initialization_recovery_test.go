//go:build integration && linux

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

func TestI06_10StartupAutomaticallyRecoversInterruptedInitialization(t *testing.T) {
	const old = "interrupted-initialization"
	d := newDeploymentBeforeStart(t, false, func(d *deployment) {
		// Seed the durable state left by an older worker after a restart found
		// an incomplete acquisition. No known completion time or usable seal.
		// The old timestamps exercise retry age without a five-minute CI sleep.
		d.sql(t, `INSERT INTO control.tasks (id,idempotency_key,mode,status,requested_by,accepted_at,started_at,summary)
 VALUES ('interrupted-initialization','system-initialize-v1','initialize','failed','scheduler',clock_timestamp()-interval '11 minutes',clock_timestamp()-interval '10 minutes','{"failure":"corrupt_seal"}');
 UPDATE control.execution SET generation=2;
 UPDATE control.schedule SET revision=1,last_started_at=clock_timestamp()-interval '10 minutes',next_due_at=clock_timestamp()+interval '110 minutes'`)
	})
	d.waitSQL(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE predecessor_id='%s' AND requested_by='scheduler'", old), "1")
	child := d.sql(t, fmt.Sprintf("SELECT id FROM control.tasks WHERE predecessor_id='%s'", old))
	d.waitTask(t, child, "success_published")
	d.waitSQL(t, "SELECT count(*) FROM control.execution WHERE task_id IS NULL", "1")
	if d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND status='failed' AND finished_at IS NULL AND summary->>'failure'='corrupt_seal'", old)) != "1" {
		t.Fatal("interrupted history was overwritten")
	}
	if d.sql(t, "SELECT count(*) FROM control.tasks") != "2" || d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.source_progress WHERE task_id='%s'", child)) != "14" {
		t.Fatal("automatic recovery duplicated or omitted sources")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, ready := d.request(t, "GET", d.workerURL+"/readyz", "", "", "")
		if status == 200 && ready["ready"] == true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker not ready after automatic initialization", ready)
		}
		time.Sleep(200 * time.Millisecond)
	}
	data := d.query(t, contract.StudentGrades, `{"student_number":"00202600001","academic_year":"all","page":1,"page_size":100}`)
	if data["total"] != float64(4) {
		t.Fatal("automatically recovered data not queryable")
	}
	d.compose(t, "restart", "worker")
	d.waitLive(t, d.workerURL)
	if d.sql(t, "SELECT count(*) FROM control.tasks") != "2" {
		t.Fatal("restart duplicated completed initialization")
	}
}
