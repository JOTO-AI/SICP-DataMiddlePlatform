//go:build integration && linux

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

const managementSession = "administrator-session"

type deployment struct {
	ctx                                                  context.Context
	root, project, queryURL, workerURL, mockURL, initial string
	client                                               *http.Client
	cookie                                               *http.Cookie
	csrf                                                 string
}

func newDeployment(t *testing.T, initialize bool, workerValues ...map[string]string) *deployment {
	t.Helper()
	return newDeploymentBeforeStart(t, initialize, nil, workerValues...)
}

func newDeploymentBeforeStart(t *testing.T, initialize bool, beforeStart func(*deployment), workerValues ...map[string]string) *deployment {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("SICP_INTEGRATION") != "1" {
		t.Fatal("deployment tests run only in GitHub Linux integration")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	d := &deployment{ctx: ctx, root: root, project: "sicp-i06-" + strings.ToLower(rand.Text()[:10]), queryURL: "http://127.0.0.1:18080", workerURL: "http://127.0.0.1:18081", client: &http.Client{Transport: transport, Timeout: 8 * time.Second}}
	d.script(t, "prepare")
	if len(workerValues) > 0 {
		path := filepath.Join(root, ".env")
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Error(err)
			}
		})
		lines := strings.Split(string(original), "\n")
		for i, line := range lines {
			key, _, _ := strings.Cut(line, "=")
			if value, ok := workerValues[0][key]; ok {
				lines[i] = key + "=" + value
			}
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		cmd := d.composeCommand(cleanup, "logs", "--no-color")
		logs, _ := cmd.CombinedOutput()
		dir := filepath.Join(d.root, "artifacts", "integration")
		_ = os.MkdirAll(dir, 0700)
		if err := os.WriteFile(filepath.Join(dir, d.project+".log"), logs, 0600); err != nil {
			t.Error(err)
		}
		if out, err := d.composeCommand(cleanup, "down", "--volumes", "--remove-orphans").CombinedOutput(); err != nil {
			t.Errorf("isolated deployment cleanup: %v: %s", err, out)
		}
	})
	// Image builds run once in this same job from its clean checkout. Every
	// numbered case gets a distinct project and brand-new named volumes.
	for _, operation := range []string{"validate", "infra", "migrate"} {
		d.script(t, operation)
	}
	id := strings.TrimSpace(d.compose(t, "ps", "-q", "mock"))
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", id)
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal("mock network inspection", err)
	}
	ip := net.ParseIP(strings.TrimSpace(string(raw)))
	if ip == nil || !ip.IsPrivate() {
		t.Fatal("mock is not on a private bridge")
	}
	d.mockURL = "http://" + ip.String() + ":8090"
	if beforeStart != nil {
		beforeStart(d)
	}
	d.script(t, "start")
	if initialize {
		d.initial = d.initialize(t)
	}
	return d
}

func (d *deployment) composeCommand(ctx context.Context, args ...string) *exec.Cmd {
	all := append([]string{"compose", "-p", d.project, "-f", filepath.Join(d.root, "compose.yaml")}, args...)
	cmd := exec.CommandContext(ctx, "docker", all...)
	cmd.Dir = d.root
	return cmd
}
func commandOutput(t *testing.T, cmd *exec.Cmd) string {
	t.Helper()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("deployment command: %v: %s", err, stderr.String())
	}
	return string(out)
}
func (d *deployment) compose(t *testing.T, args ...string) string {
	t.Helper()
	return commandOutput(t, d.composeCommand(d.ctx, args...))
}
func (d *deployment) script(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(d.ctx, "bash", append([]string{"scripts/sicp.sh"}, args...)...)
	cmd.Dir = d.root
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "SICP_PROJECT=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "SICP_PROJECT="+d.project)
	return commandOutput(t, cmd)
}
func (d *deployment) sql(t *testing.T, query string) string {
	t.Helper()
	return strings.TrimSpace(d.compose(t, "exec", "-T", "db-m", "psql", "-U", "postgres", "-d", "control", "-At", "-c", query))
}
func (d *deployment) request(t *testing.T, method, endpoint, token, key, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(d.ctx, method, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token == managementSession {
		d.authorize(t, req)
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := d.client.Do(req)
	if err != nil {
		t.Fatal("local deployment request", err)
	}
	if response.StatusCode == 401 && token == managementSession {
		response.Body.Close()
		d.cookie = nil
		req.Header.Del("Cookie")
		if req.GetBody != nil {
			req.Body, _ = req.GetBody()
		}
		d.authorize(t, req)
		response, err = d.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
	}
	defer response.Body.Close()
	var envelope map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, envelope
}
func (d *deployment) authorize(t *testing.T, req *http.Request) {
	t.Helper()
	if d.cookie == nil {
		login, _ := http.NewRequestWithContext(d.ctx, "POST", d.workerURL+"/console-api/login", strings.NewReader(`{"username":"synthetic-admin","password":"synthetic-admin-password"}`))
		login.Header.Set("Content-Type", "application/json")
		login.Header.Set("X-SICP-Request", "console")
		response, err := d.client.Do(login)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var envelope struct{ Data struct{ CSRF string } }
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil || response.StatusCode != 200 || len(response.Cookies()) != 1 {
			t.Fatal("login failed", err, response.StatusCode)
		}
		d.cookie, d.csrf = response.Cookies()[0], envelope.Data.CSRF
	}
	req.AddCookie(d.cookie)
	req.Header.Set("X-SICP-Request", "console")
	req.Header.Set("X-SICP-CSRF", d.csrf)
}
func (d *deployment) submit(t *testing.T, action, key string) string {
	t.Helper()
	status, envelope := d.request(t, "POST", d.workerURL+"/admin/v1/tasks/"+action, managementSession, key, "{}")
	if status != 202 {
		t.Fatalf("admission %s: %d %v", action, status, envelope["code"])
	}
	data := envelope["data"].(map[string]any)
	return data["task_id"].(string)
}
func (d *deployment) initialize(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		id := d.sql(t, "SELECT id FROM control.tasks WHERE idempotency_key='system-initialize-v1'")
		if id != "" {
			d.waitTask(t, id, "success_published")
			return id
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic initialization not created")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Advance the persisted timer in the isolated test database. No public sync
// endpoint exists; the production scheduler must observe and dispatch it.
func (d *deployment) scheduledSync(t *testing.T) string {
	t.Helper()
	previous := d.sql(t, "SELECT id FROM control.tasks WHERE mode='incremental' ORDER BY accepted_at DESC LIMIT 1")
	d.sql(t, "UPDATE control.schedule SET next_due_at=clock_timestamp(),revision=revision+1 WHERE singleton=TRUE")
	deadline := time.Now().Add(30 * time.Second)
	for {
		id := d.sql(t, "SELECT id FROM control.tasks WHERE mode='incremental' AND requested_by='scheduler' ORDER BY accepted_at DESC LIMIT 1")
		if id != "" && id != previous {
			return id
		}
		if time.Now().After(deadline) {
			t.Fatal("timer did not dispatch")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
func (d *deployment) waitTask(t *testing.T, id, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		status, envelope := d.request(t, "GET", d.workerURL+"/admin/v1/tasks/"+id, managementSession, "", "")
		if status != 200 {
			t.Fatalf("task status: %d %v", status, envelope["code"])
		}
		data := envelope["data"].(map[string]any)
		state := data["status"].(string)
		if state == want {
			return data
		}
		if strings.HasPrefix(state, "success_") || state == "failed" || time.Now().After(deadline) {
			t.Fatalf("task %s state=%s wanted=%s", id, state, want)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
func (d *deployment) query(t *testing.T, operation contract.Operation, body string) map[string]any {
	t.Helper()
	var path string
	for _, definition := range contract.Definitions() {
		if definition.Operation == operation {
			path = definition.Path
		}
	}
	req, err := http.NewRequestWithContext(d.ctx, "POST", d.queryURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "synthetic-query-secret")
	response, err := d.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("deployed query %s: %d %v", operation, response.StatusCode, envelope["code"])
	}
	return envelope["data"].(map[string]any)
}

func TestI06_01(t *testing.T) {
	d := newDeployment(t, true, map[string]string{"SOURCE_PAGE_SIZE": "2"})
	status, audit := d.request(t, "GET", d.mockURL+"/test/audit", "synthetic-mock-control-token", "", "")
	if status != 200 {
		t.Fatal("mock audit unavailable")
	}
	requests := audit["requests"].([]any)
	if len(requests) <= 14 {
		t.Fatal("configured page size did not paginate sources")
	}
	for _, raw := range requests {
		parameters := raw.(map[string]any)["parameters"].(map[string]any)
		if parameters["pageSize"] != "2" {
			t.Fatal("worker ignored configured source page size", parameters)
		}
	}
	for _, service := range []string{"query", "worker"} {
		id := strings.TrimSpace(d.compose(t, "ps", "-q", service))
		cmd := exec.CommandContext(d.ctx, "docker", "inspect", "--format", "{{.Config.User}} {{.HostConfig.ReadonlyRootfs}}", id)
		if got := strings.TrimSpace(commandOutput(t, cmd)); got != "65532:65532 true" {
			t.Fatal("runtime isolation", got)
		}
	}
	for _, path := range []string{"logs", "staging", "targets", "spool"} {
		if got := strings.TrimSpace(d.compose(t, "exec", "-T", "worker", "stat", "-c", "%u:%g:%a", "/var/lib/sicp/"+path)); got != "65532:65532:700" {
			t.Fatal("volume ownership", path, got)
		}
	}
	for _, op := range []contract.Operation{contract.StudentGrades, contract.BatchGrades} {
		identity := `"student_number":"00202600001"`
		if op == contract.BatchGrades {
			identity = `"student_numbers":["00202600001"]`
		}
		data := d.query(t, op, `{`+identity+`,"academic_year":"all","page":1,"page_size":100}`)
		items := data["items"].([]any)
		if data["total"] != float64(4) || len(items) != 4 {
			t.Fatal("deployed grade count", data["total"])
		}
		for i, score := range []float64{95, 70.5, 80, 63} {
			item := items[i].(map[string]any)
			if item["grade_id"] != []string{"G000001", "G000002", "G000013", "G000025"}[i] || item["score"] != score {
				t.Fatal("deployed grade answer mismatch")
			}
		}
	}
	for _, tc := range []struct {
		op        contract.Operation
		field, id string
	}{{contract.StudentTimetable, "student_number", "00202600001"}, {contract.StaffTimetable, "staff_number", "001"}} {
		data := d.query(t, tc.op, fmt.Sprintf(`{"%s":"%s","academic_year":"2026-2027","semester":"1"}`, tc.field, tc.id))
		if len(data["items"].([]any)) == 0 || data[tc.field] != tc.id {
			t.Fatal("deployed timetable empty or wrong identity")
		}
	}
	if d.sql(t, "SELECT count(*) FROM control.source_progress") != "14" {
		t.Fatal("initialization did not confirm fourteen sources")
	}
}

func TestI06_08(t *testing.T) {
	d := newDeployment(t, true)
	d.compose(t, "restart", "worker")
	deadline := time.Now().Add(30 * time.Second)
	for {
		req, _ := http.NewRequestWithContext(d.ctx, "GET", d.workerURL+"/livez", nil)
		response, err := d.client.Do(req)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("worker restart timed out")
		}
		time.Sleep(200 * time.Millisecond)
	}
	d.initialize(t)
	if d.sql(t, "SELECT count(*) FROM control.tasks WHERE mode='initialize'") != "1" {
		t.Fatal("restart duplicated bootstrap")
	}
	for _, action := range []string{"initialize", "sync"} {
		req, _ := http.NewRequestWithContext(d.ctx, "POST", d.workerURL+"/admin/v1/tasks/"+action, strings.NewReader("{}"))
		response, err := d.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 && response.StatusCode != 405 {
			t.Fatal("removed route available", action, response.StatusCode)
		}
	}
}
