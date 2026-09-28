//go:build integration && linux

package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func (d *deployment) mockPatch(t *testing.T, body string) {
	t.Helper()
	status, _ := d.request(t, "POST", d.mockURL+"/test/control", "synthetic-mock-control-token", "", body)
	if status != 200 {
		t.Fatal("mock control rejected", status)
	}
}

// Read only the acceptance headers, then drop the TCP connection without
// consuming the receipt. The server has committed before it sends HTTP 202.
func (d *deployment) acceptAndDisconnect(t *testing.T, action, key string) string {
	t.Helper()
	u, _ := url.Parse(d.workerURL)
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req, _ := http.NewRequest("POST", d.workerURL+"/admin/v1/tasks/"+action, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+managementToken)
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("Content-Type", "application/json")
	if err := req.Write(conn); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 202 {
		t.Fatal("disconnected admission", response.StatusCode)
	}
	id := strings.TrimPrefix(response.Header.Get("Location"), "/admin/v1/tasks/")
	if len(id) != 32 {
		t.Fatal("missing durable task location")
	}
	return id
}

func TestI06_02(t *testing.T) {
	credentials := `[{"operator":"synthetic-admin","token":"synthetic-management-token","actions":["initialize","sync","force_full","rerun","status"]},{"operator":"synthetic-reader","token":"synthetic-status-only-token","actions":["status"]}]`
	d := newDeployment(t, false, map[string]string{"MANAGEMENT_CREDENTIALS": credentials})
	for _, action := range []string{"initialize", "sync", "force-full", "00000000000000000000000000000000/rerun"} {
		status, _ := d.request(t, "POST", d.workerURL+"/admin/v1/tasks/"+action, "synthetic-query-secret", "denied", "{}")
		if status != 401 {
			t.Fatal("business credential accepted as admin", action, status)
		}
		status, _ = d.request(t, "POST", d.workerURL+"/admin/v1/tasks/"+action, "synthetic-status-only-token", "denied", "{}")
		if status != 403 {
			t.Fatal("status-only grant allowed mutation", action, status)
		}
	}
	req, _ := http.NewRequestWithContext(d.ctx, "POST", d.workerURL+"/admin/v1/tasks/initialize", strings.NewReader("{}"))
	req.Header.Set("X-API-Key", "synthetic-query-secret")
	req.Header.Set("Idempotency-Key", "query-header")
	response, err := d.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("business header accepted for management")
	}
	if d.sql(t, "SELECT count(*) FROM control.tasks") != "0" {
		t.Fatal("unauthorized task admitted")
	}
	id := d.initialize(t)
	status, envelope := d.request(t, "GET", d.workerURL+"/admin/v1/tasks/"+id, "synthetic-status-only-token", "", "")
	if status != 200 || envelope["data"].(map[string]any)["operator"] != "synthetic-admin" {
		t.Fatal("status permission or recorded operator incorrect")
	}
}

func TestI06_03(t *testing.T) {
	d := newDeployment(t, true)
	d.mockPatch(t, `{"delay":0.15}`)
	id := d.acceptAndDisconnect(t, "force-full", "lost-receipt")
	for range 3 {
		status, body := d.request(t, "POST", d.workerURL+"/admin/v1/tasks/force-full", managementToken, "lost-receipt", "{}")
		if status != 202 {
			t.Fatal("idempotent admission failed", status)
		}
		data := body["data"].(map[string]any)
		if data["task_id"] != id || data["created"] != false {
			t.Fatal("lost receipt created another task")
		}
	}
	d.waitTask(t, id, "success_published")
	if d.sql(t, "SELECT count(*) FROM control.tasks WHERE idempotency_key='lost-receipt'") != "1" || d.sql(t, "SELECT count(*) FROM control.publication_outbox WHERE kind='bundle_publish'") != "2" {
		t.Fatal("duplicate acceptance or publication")
	}
}

func TestI06_04(t *testing.T) {
	d := newDeployment(t, true)
	d.mockPatch(t, `{"delay":0.15}`)
	id := d.acceptAndDisconnect(t, "force-full", "disconnected-client")
	result := d.waitTask(t, id, "success_published")
	if result["started_at"] == nil || result["finished_at"] == nil || result["progress_confirmed"] != true {
		t.Fatal("background task did not complete after disconnect")
	}
	if d.sql(t, "SELECT count(*) FROM control.tasks WHERE idempotency_key='disconnected-client'") != "1" {
		t.Fatal("task missing after disconnect")
	}
	var cli map[string]any
	if err := json.Unmarshal([]byte(d.script(t, "task", id)), &cli); err != nil || cli["data"].(map[string]any)["status"] != "success_published" {
		t.Fatal("Bash task tracking", err)
	}
}

func (d *deployment) mockMail(t *testing.T) []map[string]any {
	t.Helper()
	req, _ := http.NewRequestWithContext(d.ctx, "GET", d.mockURL+"/test/mail", nil)
	req.Header.Set("Authorization", "Bearer synthetic-mock-control-token")
	response, err := d.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var mail []map[string]any
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&mail) != nil {
		t.Fatal("mock mailbox audit unavailable")
	}
	return mail
}

func TestI06_07(t *testing.T) {
	// Development must ignore configured upstream/SMTP credentials, including
	// values present in Docker Config.Env before the entrypoint runs.
	d := newDeployment(t, true, map[string]string{
		"SOURCE_ORIGIN": "https://unused-upstream.example.invalid",
		"SOURCE_APP_ID": "synthetic-unused-app", "SOURCE_APP_SECRET": "synthetic-unused-secret",
		"MAIL_ADDRESS":  "unused-smtp.example.invalid:587",
		"MAIL_USERNAME": "synthetic-unused-user", "MAIL_PASSWORD": "synthetic-unused-password",
	})
	d.mockPatch(t, `{"unknown_table":"dwd_bzksbjsjzlb"}`)
	id := d.submit(t, "force-full", "audit-failure")
	d.waitTask(t, id, "failed")
	deadline := time.Now().Add(20 * time.Second)
	for {
		mail := d.mockMail(t)
		if len(mail) > 0 {
			if len(mail) != 1 || !strings.Contains(mail[0]["body"].(string), "operator@example.invalid") {
				t.Fatal("unexpected mock mail target/count")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("final failure did not reach local Mock SMTP")
		}
		time.Sleep(200 * time.Millisecond)
	}
	status, audit := d.request(t, "GET", d.mockURL+"/test/audit", "synthetic-mock-control-token", "", "")
	if status != 200 {
		t.Fatal("mock audit unavailable")
	}
	tables := map[string]bool{}
	for _, value := range audit["requests"].([]any) {
		request := value.(map[string]any)
		if request["host"] != "mock-source:8090" {
			t.Fatal("source request used unexpected target")
		}
		tables[request["table"].(string)] = true
		if _, exposed := request["parameters"].(map[string]any)["access_token"]; exposed {
			t.Fatal("audit leaked source token")
		}
	}
	if len(tables) != 14 {
		t.Fatal("audit did not cover every source")
	}
	loads := audit["data_loads"].([]any)
	if len(loads) < 14 {
		t.Fatal("missing actual fixture read audit")
	}
	for _, value := range loads {
		path := value.(string)
		if !strings.HasPrefix(path, "/app/mock_data/snapshots/initial/dwd_") || !strings.HasSuffix(path, ".json") || strings.Contains(path, "source_samples") {
			t.Fatal("unexpected dataset load")
		}
	}
	for _, service := range []string{"query", "worker", "mock"} {
		container := strings.TrimSpace(d.compose(t, "ps", "-q", service))
		raw := commandOutput(t, exec.CommandContext(d.ctx, "docker", "inspect", container))
		var inspected []struct {
			Mounts []struct {
				Source, Destination string
				RW                  bool
			}
		}
		if err := json.Unmarshal([]byte(raw), &inspected); err != nil || len(inspected) != 1 {
			t.Fatal("deployment audit", err)
		}
		for _, mount := range inspected[0].Mounts {
			if strings.Contains(mount.Source, "source_samples") || strings.Contains(mount.Destination, "source_samples") {
				t.Fatal("production sample mount detected")
			}
			if service == "mock" && mount.Destination == "/app/mock_data" && mount.RW {
				t.Fatal("mock dataset is writable")
			}
		}
		if service == "worker" {
			// Docker inspect reports the original container configuration. Read
			// PID 1 after the entrypoint's exec to audit what the worker uses.
			environment := d.compose(t, "exec", "-T", "worker", "cat", "/proc/1/environ")
			values := map[string]string{}
			for _, item := range strings.Split(environment, "\x00") {
				if k, v, ok := strings.Cut(item, "="); ok {
					values[k] = v
				}
			}
			for key, expected := range map[string]string{
				"ENVIRONMENT": "development", "SOURCE_ORIGIN": "http://mock-source:8090",
				"SOURCE_TOKEN": "synthetic-source-token", "SOURCE_SCOPE": "synthetic-sicp-source",
				"MAIL_ADDRESS": "mock-mail:1025", "MAIL_TO": "operator@example.invalid",
				"MAIL_FROM": "worker@example.invalid", "MAIL_TLS": "none",
				"DATABASE_USER": "sicp_worker", "DATABASE_LOG_USER": "sicp_logger",
			} {
				if values[key] != expected {
					t.Fatal("worker used unexpected runtime configuration", key)
				}
			}
			for _, key := range []string{"SOURCE_APP_ID", "SOURCE_APP_SECRET", "MAIL_USERNAME", "MAIL_PASSWORD"} {
				if values[key] != "" {
					t.Fatal("development retained external credentials", key)
				}
			}
			// Credentials are separate values now; only endpoint keys are URLs.
			// Enumerate required URLs so a missing dependency cannot pass silently.
			for key, host := range map[string]string{
				"DATABASE_M0_URL": "db-m", "DATABASE_M1_URL": "db-m",
				"DATABASE_S0_URL": "db-s", "DATABASE_S1_URL": "db-s",
				"DATABASE_CONTROL_URL": "db-m", "DATABASE_LOG_URL": "db-log", "REDIS_URL": "redis",
			} {
				u, err := url.Parse(values[key])
				if err != nil || u.Hostname() != host {
					t.Fatal("missing or unexpected dependency endpoint", key)
				}
			}
		}
	}
	for d.sql(t, fmt.Sprintf(`SELECT count(*) FROM control.notification_outbox
WHERE task_id='%s' AND status='accepted' AND sent_at IS NOT NULL AND attempts=1
AND payload->'delivery'->'attempts'->0->>'result'='accepted'
AND NOT (payload->'delivery' ? 'delivered_at')`, id)) != "1" {
		if time.Now().After(deadline) {
			t.Fatal("notification acknowledgement not persisted")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
