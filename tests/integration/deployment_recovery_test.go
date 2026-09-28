//go:build integration && linux

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func (d *deployment) active(t *testing.T) control.ActiveMetadata {
	t.Helper()
	raw := d.compose(t, "exec", "-T", "redis", "redis-cli", "--raw", "GET", publication.ActiveKey)
	var active control.ActiveMetadata
	if json.Unmarshal([]byte(raw), &active) != nil || active.Validate() != nil {
		t.Fatal("invalid deployed active metadata")
	}
	return active
}

func (d *deployment) progress(t *testing.T) string {
	t.Helper()
	return d.sql(t, "SELECT jsonb_agg(to_jsonb(p) ORDER BY source)::text FROM control.source_progress p")
}

func (d *deployment) waitSQL(t *testing.T, query, want string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		got := d.sql(t, query)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("deployment state timeout: got %q want %q", got, want)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (d *deployment) blockConfirmation(t *testing.T) {
	t.Helper()
	d.sql(t, `CREATE FUNCTION control.integration_confirmation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.kind='bundle_publish' AND NEW.status='confirmed' THEN
RAISE EXCEPTION 'synthetic confirmation failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER integration_confirmation BEFORE UPDATE ON control.publication_outbox
FOR EACH ROW EXECUTE FUNCTION control.integration_confirmation()`)
}

func (d *deployment) unblockConfirmation(t *testing.T) {
	t.Helper()
	d.sql(t, "DROP TRIGGER integration_confirmation ON control.publication_outbox; DROP FUNCTION control.integration_confirmation()")
}

func (d *deployment) pendingPublication(t *testing.T, key string) (string, control.ActiveMetadata) {
	t.Helper()
	d.blockConfirmation(t)
	id := d.submit(t, "force-full", key)
	d.waitSQL(t, fmt.Sprintf("SELECT count(*) FROM control.publication_outbox WHERE task_id='%s' AND kind='bundle_publish' AND status='pending'", id), "1")
	deadline := time.Now().Add(20 * time.Second)
	for {
		active := d.active(t)
		if active.EventID == id+"-publish" && active.TaskID == id {
			return id, active
		}
		if time.Now().After(deadline) {
			t.Fatal("pending publication never applied in Redis")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (d *deployment) assertBusy(t *testing.T, key string) {
	t.Helper()
	status, body := d.request(t, "POST", d.workerURL+"/admin/v1/tasks/force-full", managementToken, key, "{}")
	if status != 409 || body["code"] != "busy" || d.sql(t, "SELECT count(*) FROM control.tasks") != "2" {
		t.Fatal("pending recovery admitted conflicting task", status, body["code"])
	}
}

func TestI06_05(t *testing.T) {
	d := newDeployment(t, true)
	progress := d.progress(t)
	id, active := d.pendingPublication(t, "restart-pending")
	started := d.sql(t, fmt.Sprintf("SELECT started_at::text FROM control.tasks WHERE id='%s'", id))
	generation := d.sql(t, fmt.Sprintf("SELECT owner_generation::text FROM control.tasks WHERE id='%s'", id))
	if started == "" || generation == "" || d.progress(t) != progress || d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND finished_at IS NULL", id)) != "1" {
		t.Fatal("pending task prematurely completed")
	}
	d.assertBusy(t, "before-restart")
	d.compose(t, "stop", "query", "worker")
	d.compose(t, "restart", "db-m", "db-s", "db-log", "redis", "mock")
	d.compose(t, "up", "-d", "--wait", "--wait-timeout", "90", "db-m", "db-s", "db-log", "redis")
	// Restarting several containers can reassign bridge addresses. Resolve the
	// new Mock address and wait for HTTP readiness before auditing recovery.
	d.mockURL = "http://" + d.privateIP(t, "mock") + ":8090"
	d.waitLive(t, d.mockURL)
	// Named volumes retain the trigger and unresolved publication through restart.
	if d.active(t) != active || d.progress(t) != progress {
		t.Fatal("dependency restart lost durable publication or progress")
	}
	d.script(t, "start")
	d.assertBusy(t, "after-restart")
	if d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.publication_outbox WHERE task_id='%s' AND status='pending'", id)) != "1" {
		t.Fatal("confirmation fault not retained across database restart")
	}
	d.unblockConfirmation(t)
	result := d.waitTask(t, id, "success_published")
	if result["progress_confirmed"] != true || d.active(t) != active || d.sql(t, fmt.Sprintf("SELECT started_at::text FROM control.tasks WHERE id='%s'", id)) != started {
		t.Fatal("restart recovery rewrote task start, metadata or progress")
	}
	if d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND owner_generation>%s AND finished_at IS NOT NULL", id, generation)) != "1" || d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.source_progress WHERE task_id='%s'", id)) != "14" {
		t.Fatal("recovery did not claim new ownership and confirm all sources")
	}
	status, audit := d.request(t, "GET", d.mockURL+"/test/audit", "synthetic-mock-control-token", "", "")
	if status != 200 || len(audit["requests"].([]any)) != 0 {
		t.Fatal("sealed restart recovery re-fetched source data")
	}
	if d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.publication_outbox WHERE task_id='%s' AND kind='bundle_publish' AND status='confirmed'", id)) != "1" {
		t.Fatal("publication not confirmed exactly once")
	}
	data := d.query(t, contract.StudentGrades, `{"student_number":"00202600001","academic_year":"all","page":1,"page_size":100}`)
	if data["total"] != float64(4) || len(data["items"].([]any)) != 4 {
		t.Fatal("deployed query did not recover")
	}
	if strings.TrimSpace(d.compose(t, "exec", "-T", "redis", "redis-cli", "TTL", publication.ActiveKey)) != "-1" {
		t.Fatal("recovered active metadata acquired a TTL")
	}
}
