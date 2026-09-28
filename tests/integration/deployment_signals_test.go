//go:build integration && linux

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func (d *deployment) waitLive(t *testing.T, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		req, _ := http.NewRequestWithContext(d.ctx, "GET", endpoint+"/livez", nil)
		response, err := d.client.Do(req)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("service did not become live after process start")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func assertShutdown(t *testing.T, output string) {
	t.Helper()
	found := 0
	for _, line := range strings.Split(output, "\n") {
		var event struct {
			Kind      string
			Resources []struct{ Name, State string }
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Kind != "shutdown" {
			continue
		}
		found++
		if len(event.Resources) != 3 {
			t.Fatal("incomplete service shutdown record")
		}
		for i, name := range []string{"http", "application", "runtime"} {
			if event.Resources[i].Name != name || event.Resources[i].State != "closed" {
				t.Fatal("service did not close resources in order")
			}
		}
	}
	if found != 1 {
		t.Fatal("signal did not reach Go shutdown handler exactly once")
	}
}

type directService struct {
	cmd    *exec.Cmd
	done   chan error
	waited bool
	log    string
}

func (d *deployment) startDirect(t *testing.T, binary string, env []string, role string, attempt int) *directService {
	t.Helper()
	path := filepath.Join(d.root, "artifacts", "integration", fmt.Sprintf("%s-%s-%d.log", d.project, role, attempt))
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	p := &directService{cmd: exec.CommandContext(d.ctx, binary), done: make(chan error, 1), log: path}
	p.cmd.Env, p.cmd.Stdout, p.cmd.Stderr = env, output, output
	if err := p.cmd.Start(); err != nil {
		output.Close()
		t.Fatal(err)
	}
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() {
		if !p.waited {
			_ = p.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-p.done:
			case <-time.After(25 * time.Second):
				_ = p.cmd.Process.Kill()
				<-p.done
			}
		}
		_ = output.Close()
	})
	endpoint := d.queryURL
	if role == "worker" {
		endpoint = d.workerURL
	}
	d.waitLive(t, endpoint)
	return p
}

func (p *directService) signal(t *testing.T, signal syscall.Signal) {
	t.Helper()
	started := time.Now()
	if err := p.cmd.Process.Signal(signal); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-p.done:
		p.waited = true
		if err != nil {
			t.Fatal("service signal exit was not successful", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("service exceeded configured 20s shutdown budget plus tolerance")
	}
	if time.Since(started) > 25*time.Second {
		t.Fatal("unbounded service exit")
	}
	raw, err := os.ReadFile(p.log)
	if err != nil {
		t.Fatal(err)
	}
	assertShutdown(t, string(raw))
}

func (d *deployment) privateIP(t *testing.T, service string) string {
	t.Helper()
	id := strings.TrimSpace(d.compose(t, "ps", "-q", service))
	raw := commandOutput(t, exec.CommandContext(d.ctx, "docker", "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", id))
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || !ip.IsPrivate() {
		t.Fatal("dependency not on isolated private bridge", service)
	}
	return ip.String()
}

// Reuse the exact built service and its stopped durable files on the Linux
// host. Only fixture endpoints, bind address and filesystem roots are remapped.
func (d *deployment) directConfig(t *testing.T, role string) (string, []string) {
	t.Helper()
	id := strings.TrimSpace(d.compose(t, "ps", "-q", role))
	d.compose(t, "stop", role)
	dir := t.TempDir()
	binary := filepath.Join(dir, "service")
	commandOutput(t, exec.CommandContext(d.ctx, "docker", "cp", id+":/usr/local/bin/service", binary))
	if err := os.Chmod(binary, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(d.root, "internal", "assembly", "testdata", "deployment", role+".env"))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok && !strings.HasPrefix(key, "#") {
			values[key] = value
		}
	}
	for _, item := range []struct{ key, path string }{{"LOG_DIRECTORY", "logs"}, {"STAGING_DIRECTORY", "staging"}, {"TARGET_DIRECTORY", "targets"}, {"SPOOL_DIRECTORY", "spool"}} {
		if _, ok := values[item.key]; !ok {
			continue
		}
		path := filepath.Join(dir, item.path)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		// Without -a, docker cp assigns copies to the invoking host user.
		commandOutput(t, exec.CommandContext(d.ctx, "docker", "cp", id+":/var/lib/sicp/"+item.path+"/.", path))
		values[item.key] = path
	}
	hosts := map[string]string{}
	for _, service := range []string{"db-m", "db-s", "db-log", "redis"} {
		hosts[service] = d.privateIP(t, service)
	}
	for key, value := range values {
		if (strings.HasPrefix(key, "DATABASE_") && strings.HasSuffix(key, "_URL")) || key == "REDIS_URL" {
			u, err := url.Parse(value)
			if err != nil || hosts[u.Hostname()] == "" || u.Port() == "" {
				t.Fatal("unexpected direct-process fixture endpoint", key)
			}
			u.Host = net.JoinHostPort(hosts[u.Hostname()], u.Port())
			values[key] = u.String()
		}
	}
	endpoint := d.queryURL
	if role == "worker" {
		endpoint = d.workerURL
		values["SOURCE_ORIGIN"] = d.mockURL
		mock, _ := url.Parse(d.mockURL)
		values["MAIL_ADDRESS"] = net.JoinHostPort(mock.Hostname(), "1025")
	}
	u, _ := url.Parse(endpoint)
	values["HTTP_ADDRESS"] = u.Host
	var env []string
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return binary, env
}

func (d *deployment) sourceAuditCount(t *testing.T) int {
	t.Helper()
	status, audit := d.request(t, "GET", d.mockURL+"/test/audit", "synthetic-mock-control-token", "", "")
	if status != 200 {
		t.Fatal("source audit unavailable")
	}
	return len(audit["requests"].([]any))
}

func TestI06_09(t *testing.T) {
	for _, entry := range []string{"direct", "container"} {
		for _, role := range []string{"query", "worker"} {
			for _, signal := range []struct {
				name  string
				value syscall.Signal
			}{{"SIGTERM", syscall.SIGTERM}, {"SIGINT", syscall.SIGINT}} {
				t.Run(entry+"/"+role+"/"+signal.name, func(t *testing.T) {
					d := newDeployment(t, true)
					d.waitSQL(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND owner_id IS NULL", d.initial), "1")
					var process *directService
					var binary string
					var env []string
					if entry == "direct" {
						binary, env = d.directConfig(t, role)
						process = d.startDirect(t, binary, env, role, 1)
					}
					var task, started, generation, progress string
					var active control.ActiveMetadata
					if role == "worker" {
						progress = d.progress(t)
						task, active = d.pendingPublication(t, "signal-pending")
						started = d.sql(t, fmt.Sprintf("SELECT started_at::text FROM control.tasks WHERE id='%s'", task))
						generation = d.sql(t, fmt.Sprintf("SELECT owner_generation::text FROM control.tasks WHERE id='%s'", task))
					} else {
						d.query(t, contract.StudentGrades, `{"student_number":"00202600001","academic_year":"all","page":1,"page_size":100}`)
					}
					requests := d.sourceAuditCount(t)
					if entry == "direct" {
						process.signal(t, signal.value)
					} else {
						id := strings.TrimSpace(d.compose(t, "ps", "-q", role))
						commandOutput(t, exec.CommandContext(d.ctx, "docker", "update", "--restart=no", id))
						stop, cancel := context.WithTimeout(d.ctx, 25*time.Second)
						defer cancel()
						commandOutput(t, exec.CommandContext(stop, "docker", "kill", "--signal", signal.name, id))
						if strings.TrimSpace(commandOutput(t, exec.CommandContext(stop, "docker", "wait", id))) != "0" {
							t.Fatal("container signal exit was not successful")
						}
						assertShutdown(t, commandOutput(t, exec.CommandContext(d.ctx, "docker", "logs", id)))
					}
					if role == "worker" {
						if d.active(t) != active || d.progress(t) != progress || d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND finished_at IS NULL", task)) != "1" || d.sql(t, "SELECT count(*) FROM control.publication_outbox WHERE status='pending'") != "1" {
							t.Fatal("signal erased or falsely completed pending task")
						}
						d.unblockConfirmation(t)
					}
					if entry == "direct" {
						d.startDirect(t, binary, env, role, 2)
					} else {
						d.compose(t, "up", "-d", "--no-deps", "--wait", "--wait-timeout", "60", role)
					}
					if role == "worker" {
						d.waitTask(t, task, "success_published")
						if d.active(t) != active || d.sql(t, fmt.Sprintf("SELECT started_at::text FROM control.tasks WHERE id='%s'", task)) != started || d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.tasks WHERE id='%s' AND owner_generation>%s", task, generation)) != "1" || d.sql(t, fmt.Sprintf("SELECT count(*) FROM control.source_progress WHERE task_id='%s'", task)) != "14" {
							t.Fatal("signal restart did not recover original task facts")
						}
					}
					if d.sourceAuditCount(t) != requests {
						t.Fatal("signal restart unexpectedly fetched source data")
					}
					data := d.query(t, contract.StudentGrades, `{"student_number":"00202600001","academic_year":"all","page":1,"page_size":100}`)
					if data["total"] != float64(4) {
						t.Fatal("query not usable after signal restart")
					}
				})
			}
		}
	}
}
