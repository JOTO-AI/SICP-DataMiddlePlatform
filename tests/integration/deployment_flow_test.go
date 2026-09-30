//go:build integration && linux

package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func (d *deployment) assertTaskLog(t *testing.T, id, outcome string) {
	t.Helper()
	// Independently derive the documented audit correlation for the synthetic key.
	key := sha256.Sum256([]byte("synthetic-audit-key-0000000000000"))
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte(id))
	entity := hex.EncodeToString(mac.Sum(nil))
	deadline := time.Now().Add(20 * time.Second)
	query := fmt.Sprintf("SELECT payload::text FROM logs.events WHERE kind='task' AND entity_id='%s' ORDER BY phase", entity)
	for {
		raw := strings.TrimSpace(d.compose(t, "exec", "-T", "db-log", "psql", "-U", "postgres", "-d", "logs", "-At", "-c", query))
		lines := strings.Split(raw, "\n")
		if len(lines) == 2 {
			phases := map[string]bool{}
			for _, line := range lines {
				var row struct {
					Event struct {
						Phase, Outcome string
						At             time.Time `json:"occurred_at"`
					} `json:"event"`
				}
				if json.Unmarshal([]byte(line), &row) != nil || phases[row.Event.Phase] {
					t.Fatal("invalid or duplicated task log")
				}
				phases[row.Event.Phase] = true
				column := "started_at"
				if row.Event.Phase == "finished" {
					column = "finished_at"
					if row.Event.Outcome != outcome {
						t.Fatal("task log outcome mismatch")
					}
				} else if row.Event.Phase != "started" || row.Event.Outcome != "" {
					t.Fatal("unexpected task log phase")
				}
				if d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND %s='%s'::timestamptz", id, column, row.Event.At.Format(time.RFC3339Nano))) != "1" {
					t.Fatal("task log time differs from durable task fact")
				}
			}
			return
		}
		if len(lines) > 2 || time.Now().After(deadline) {
			t.Fatal("task start/end did not reach real log database exactly once")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (d *deployment) assertFlowState(t *testing.T, task, bundle string, instance storage.Instance, score float64, contacts int, deleted bool, retainedProgress ...string) control.ActiveMetadata {
	t.Helper()
	active := d.active(t)
	if active.Position.BundleID != "C"+bundle || active.Position.Position.Instance != instance {
		t.Fatal("unexpected flow bundle or query instance")
	}
	// Each copy is read independently through its physical database.
	for _, copy := range []struct{ instance, service string }{{"M", "db-m"}, {"S", "db-s"}} {
		slot := d.sql(t, fmt.Sprintf("SELECT slot FROM control.slots WHERE instance='%s' AND bundle_id='C%s' AND state='ready'", copy.instance, bundle))
		if slot != "0" && slot != "1" {
			t.Fatal("missing complete physical copy", copy.instance)
		}
		query := `SELECT count(*)::text || '|' || count(*) FILTER (WHERE "XH"='00202600003')::text FROM source.dwd_bzksgrtxsjzlb`
		present := 1
		if deleted {
			present = 0
		}
		got := strings.TrimSpace(d.compose(t, "exec", "-T", copy.service, "psql", "-U", "postgres", "-d", "slot"+slot, "-At", "-c", query))
		if got != fmt.Sprintf("%d|%d", contacts, present) {
			t.Fatal("physical contact answer mismatch", copy.instance, got)
		}
	}
	for _, op := range []contract.Operation{contract.StudentGrades, contract.BatchGrades} {
		identity := `"student_number":"00202600003"`
		if op == contract.BatchGrades {
			identity = `"student_numbers":["00202600003"]`
		}
		data := d.query(t, op, `{`+identity+`,"academic_year":"all","page":1,"page_size":100}`)
		items := data["items"].([]any)
		if data["total"] != float64(3) || len(items) != 3 {
			t.Fatal("flow grade count mismatch")
		}
		for i, want := range []float64{score, 81, 66} {
			item := items[i].(map[string]any)
			if item["grade_id"] != []string{"G000003", "G000015", "G000027"}[i] || item["score"] != want {
				t.Fatal("flow grade answer mismatch", op)
			}
		}
	}
	// All pages above were consumed. Accelerate only these fixture snapshots'
	// expiry, so this single flow need not sleep through production token TTLs.
	d.sql(t, "UPDATE control.query_snapshots SET expires_at=created_at+interval '1 microsecond'")
	progressTask := task
	if len(retainedProgress) > 0 {
		progressTask = retainedProgress[0]
	}
	if d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.source_progress p JOIN control.tasks t ON t.id='%s' WHERE p.task_id='%s' AND p.confirmed_until=t.started_at", progressTask, task)) != "14" {
		t.Fatal("flow progress differs from fixed task start")
	}
	d.waitSQL(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND owner_id IS NULL", task), "1")
	d.assertTaskLog(t, task, "succeeded")
	return active
}

func resultCounts(t *testing.T, result map[string]any) control.ResultSummary {
	t.Helper()
	raw, err := json.Marshal(result["counts"])
	var counts control.ResultSummary
	if err != nil || json.Unmarshal(raw, &counts) != nil || counts.Validate() != nil {
		t.Fatal("missing valid task counts")
	}
	return counts
}

func TestI06_06(t *testing.T) {
	d := newDeployment(t, true)
	d.assertFlowState(t, d.initial, d.initial, storage.Standby, 87, 12, false)
	initialProgress := d.progress(t)
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Second).In(zone).Format("2006-01-02 15:04:05.000000")
	d.mockPatch(t, fmt.Sprintf(`{"snapshot":"updated","stamp":"%s"}`, stamp))
	changed := d.scheduledSync(t)
	d.waitTask(t, changed, "success_published")
	active := d.assertFlowState(t, changed, changed, storage.Main, 88.5, 13, false)
	if d.progress(t) == initialProgress {
		t.Fatal("changed flow failed to advance progress")
	}
	unchanged := d.scheduledSync(t)
	result := d.waitTask(t, unchanged, "success_unchanged")
	if result["published_at"] != nil || result["checked_at"] == nil || d.assertFlowState(t, unchanged, changed, storage.Main, 88.5, 13, false) != active {
		t.Fatal("unchanged flow republished or moved")
	}
	counts := resultCounts(t, result)
	for _, source := range counts.Sources {
		if source.Added != 0 || source.Modified != 0 || source.Removed != 0 {
			t.Fatal("unchanged flow reported source changes")
		}
	}
	for _, copy := range counts.Copies {
		if copy.WrittenRows != 0 || copy.RebuiltTables != 0 {
			t.Fatal("unchanged flow rebuilt physical data")
		}
	}
	d.mockPatch(t, `{"snapshot":"after_delete"}`)
	full := d.submit(t, "force-full", "flow-full")
	result = d.waitTask(t, full, "success_published")
	active = d.assertFlowState(t, full, full, storage.Standby, 88.5, 12, true)
	counts = resultCounts(t, result)
	if c := counts.Sources["dwd_bzksgrtxsjzlb"]; c.Removed != 1 || !c.DeletionsKnown || c.Before != 13 || c.After != 12 {
		t.Fatal("full flow did not count real deletion")
	}
	if len(d.mockMail(t)) != 0 || d.sql(t, "SELECT count(*) FROM control.notification_outbox") != "0" {
		t.Fatal("successful flow generated failure notice")
	}
	progress := d.progress(t)
	d.mockPatch(t, `{"unknown_table":"dwd_bzksbjsjzlb"}`)
	failed := d.submit(t, "force-full", "flow-fault")
	result = d.waitTask(t, failed, "failed")
	d.waitSQL(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND owner_id IS NULL", failed), "1")
	if result["failure"] != "acquisition_failed" || result["published_at"] != nil || result["progress_confirmed"] != false || d.active(t) != active || d.progress(t) != progress {
		t.Fatal("failed flow changed published data or success progress")
	}
	d.assertTaskLog(t, failed, "failed")
	d.waitSQL(t, fmt.Sprintf("SELECT count(*) FROM control.notification_outbox WHERE task_id='%s' AND status='accepted' AND attempts=1 AND sent_at IS NOT NULL", failed), "1")
	mail := d.mockMail(t)
	if len(mail) != 1 || strings.Contains(mail[0]["body"].(string), "synthetic-private-detail") {
		t.Fatal("failed flow notice missing, duplicated or leaked source value")
	}
	historyQuery := fmt.Sprintf("SELECT to_jsonb(t)::text FROM control.tasks t WHERE id='%s'", failed)
	history := d.sql(t, historyQuery)
	d.mockPatch(t, `{"unknown_table":null}`)
	rerun := d.submit(t, failed+"/rerun", "flow-rerun")
	result = d.waitTask(t, rerun, "success_published")
	if rerun == failed || result["predecessor_id"] != failed || result["mode"] != "force_full" {
		t.Fatal("rerun did not create a linked full task")
	}
	d.assertFlowState(t, rerun, rerun, storage.Main, 88.5, 12, true, full)
	if d.sql(t, historyQuery) != history || len(d.mockMail(t)) != 1 || d.sql(t, "SELECT count(*) FROM control.tasks") != "6" || d.sql(t, "SELECT count(*) FROM control.publication_outbox WHERE kind='bundle_publish' AND status='confirmed'") != "4" {
		t.Fatal("rerun rewrote failed history or duplicated side effects")
	}
	counts = resultCounts(t, result)
	for _, copy := range counts.Copies {
		if copy.RebuiltTables != 14 || copy.WrittenRows != 149 {
			t.Fatal("full rerun did not rebuild both complete copies")
		}
	}
}
