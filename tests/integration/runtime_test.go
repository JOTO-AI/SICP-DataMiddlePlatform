//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/application"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// Namespace changes transport keys only; Redis still executes production Lua
// and cache operations. Each case owns a prefix and never flushes shared Redis.
type scopedRedis struct {
	raw       *adapters.Redis
	prefix    string
	beforeSet func(context.Context, string) error
}

func (r *scopedRedis) Close(context.Context) error { return nil }
func (r *scopedRedis) Do(ctx context.Context, command string, args ...any) (any, error) {
	args = append([]any(nil), args...)
	if command == "SET" && r.beforeSet != nil {
		if err := r.beforeSet(ctx, args[0].(string)); err != nil {
			return nil, err
		}
	}
	switch command {
	case "GET", "SET":
		args[0] = r.prefix + args[0].(string)
	case "EVAL":
		if args[1] != 1 {
			return nil, errors.New("integration expects one Lua key")
		}
		args[2] = r.prefix + args[2].(string)
	case "PING":
	default:
		return nil, errors.New("unexpected integration Redis command")
	}
	return r.raw.Do(ctx, command, args...)
}

type mockSource struct {
	mu       sync.Mutex
	rows     map[string][]json.RawMessage
	server   *httptest.Server
	requests int
}

func newSource(t *testing.T) *mockSource {
	t.Helper()
	m := &mockSource{rows: map[string][]json.RawMessage{}}
	files, err := filepath.Glob("../../mock_data/snapshots/initial/dwd_*.json")
	if err != nil || len(files) != 14 {
		t.Fatal("fourteen synthetic source fixtures required")
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		m.rows[strings.TrimSuffix(filepath.Base(file), ".json")] = rows
	}
	raw, err := os.ReadFile("../fixtures/phase1/query/input.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m.rows); err != nil {
		t.Fatal(err)
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.requests++
		table := strings.TrimPrefix(r.URL.Path, "/apizt/openapi/invoke/sjzc/release/")
		rows, ok := m.rows[table]
		page, e1 := strconv.Atoi(r.URL.Query().Get("page"))
		size, e2 := strconv.Atoi(r.URL.Query().Get("pageSize"))
		if !ok || e1 != nil || e2 != nil || page < 1 || size < 1 || size > 1000 || r.URL.Query().Get("access_token") != "synthetic-source-token" {
			http.Error(w, "invalid synthetic source request", 400)
			return
		}
		start := min((page-1)*size, len(rows))
		end := min(start+size, len(rows))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "ok", "data": map[string]any{"total": len(rows), "list": rows[start:end]}})
	}))
	t.Cleanup(m.server.Close)
	return m
}

type runtimeFixture struct {
	e             *environment
	source        *mockSource
	redis         *scopedRedis
	worker, query *application.Application
	wr, qr        *assembly.Runtime
	wc, qc        config.Runtime
	server        *httptest.Server
}

func newRuntime(t *testing.T, initialize bool) *runtimeFixture {
	t.Helper()
	e := newEnvironment(t)
	e.migrate(t)
	f := &runtimeFixture{e: e, source: newSource(t)}
	mail := newReceiver(t)
	host := os.Getenv("SICP_TEST_REDIS_HOST")
	if ip := net.ParseIP(host); ip == nil || !ip.IsPrivate() {
		t.Fatal("explicit private Redis container required")
	}
	redisURL := "redis://" + net.JoinHostPort(host, "6379") + "/0"
	raw, err := adapters.OpenRedis(e.ctx, redisURL, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := raw.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	f.redis = &scopedRedis{raw: raw, prefix: "it:" + rand.Text() + ":"}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		keys, err := raw.Do(ctx, "KEYS", f.redis.prefix+"*")
		if err != nil {
			t.Error(err)
			return
		}
		for _, key := range keys.([]any) {
			if _, err := raw.Do(ctx, "DEL", key); err != nil {
				t.Error(err)
			}
		}
	})
	for _, role := range []config.Role{config.Worker, config.Query} {
		name := "worker"
		if role == config.Query {
			name = "query"
		}
		file, err := os.Open("../../deploy/examples/" + name + ".env.example")
		if err != nil {
			t.Fatal(err)
		}
		values, err := config.ReadEnv(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		keys := []string{"DATABASE_M0_URL", "DATABASE_M1_URL", "DATABASE_S0_URL", "DATABASE_S1_URL", "DATABASE_CONTROL_URL", "DATABASE_LOG_URL"}
		for i, key := range keys {
			u, _ := url.Parse(e.dsns[i])
			user := "sicp_worker"
			if role == config.Query {
				user = "sicp_query"
			}
			if i == 5 {
				user = "sicp_logger"
			}
			u.User = url.UserPassword(user, "synthetic")
			values[key] = u.String()
		}
		root := t.TempDir()
		for _, pair := range []struct{ key, dir string }{{"LOG_DIRECTORY", "logs"}, {"STAGING_DIRECTORY", "staging"}, {"TARGET_DIRECTORY", "targets"}, {"SPOOL_DIRECTORY", "spool"}} {
			path := filepath.Join(root, pair.dir)
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			values[pair.key] = path
		}
		values["REDIS_URL"] = redisURL
		values["SOURCE_ORIGIN"] = f.source.server.URL
		values["MAIL_ADDRESS"] = mail.listener.Addr().String()
		c, err := config.LoadRuntime(role, func(k string) (string, bool) { v, ok := values[k]; return v, ok })
		if err != nil {
			t.Fatal(err)
		}
		factories := assembly.RealFactories()
		factories.Redis = func(context.Context, config.Runtime) (assembly.Redis, error) { return f.redis, nil }
		runtime, _, err := assembly.Open(e.ctx, c, factories)
		if err != nil {
			t.Fatalf("runtime assembly: %v", err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := runtime.Close(ctx); err != nil {
				t.Error(err)
			}
		})
		app, err := application.New(c, runtime)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := app.Close(ctx); err != nil {
				t.Error(err)
			}
		})
		if role == config.Worker {
			f.worker = app
			f.wr = runtime
			f.wc = c
		} else {
			f.query = app
			f.qr = runtime
			f.qc = c
		}
	}
	mux := http.NewServeMux()
	for _, definition := range contract.Definitions() {
		mux.Handle(definition.Path, f.query.Queries[definition.Operation])
	}
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	if initialize {
		f.run(t, "initialize", "init")
	}
	return f
}
func (f *runtimeFixture) run(t *testing.T, mode, id string) {
	t.Helper()
	_, err := f.wr.Control.Admit(f.e.ctx, control.Request{ID: id, IdempotencyKey: id, Mode: mode, RequestedBy: "synthetic", AcceptedAt: realClock{}.Now()})
	if err != nil {
		t.Fatalf("admission: %v", err)
	}
	if err := f.worker.Engine.Cycle(f.e.ctx); err != nil {
		// State names are safe synthetic diagnostics; do not dump source rows,
		// transport credentials or arbitrary persisted payloads on failure.
		conn := f.e.connection(t, 4)
		rows, diagnostic := conn.Query(f.e.ctx, "SELECT name FROM control.checkpoints ORDER BY name")
		if diagnostic == nil {
			for rows.Next() {
				var name string
				if rows.Scan(&name) == nil {
					t.Logf("retained checkpoint: %s", name)
				}
			}
			rows.Close()
		}
		t.Fatalf("task %s: %v", mode, err)
	}
	task := control.Task{ID: id}
	if err := f.wr.Control.Load(f.e.ctx, &task); err != nil || task.Status != "success_published" && task.Status != "success_unchanged" {
		t.Fatalf("task %s status=%s error=%v", mode, task.Status, err)
	}
}
func (f *runtimeFixture) queryJSON(t *testing.T, op contract.Operation, body, token, key string) (int, map[string]any, string) {
	t.Helper()
	path := ""
	for _, d := range contract.Definitions() {
		if d.Operation == op {
			path = d.Path
		}
	}
	req, err := http.NewRequestWithContext(f.e.ctx, "POST", f.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", key)
	if token != "" {
		req.Header.Set(snapshot.Header, token)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope map[string]any
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, envelope, response.Header.Get(snapshot.Header)
}
func (f *runtimeFixture) queryOK(t *testing.T, op contract.Operation, body, token string) (map[string]any, string) {
	t.Helper()
	status, envelope, next := f.queryJSON(t, op, body, token, f.qc.Service.APIKey)
	if status != 200 {
		t.Fatalf("query %s: status=%d code=%v", op, status, envelope["code"])
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatal("missing response data")
	}
	return data, next
}
func rawRow(text string) source.SourceRow {
	var row source.SourceRow
	if err := json.Unmarshal([]byte(text), &row); err != nil {
		panic(err)
	}
	return row
}
