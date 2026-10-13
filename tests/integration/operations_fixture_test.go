//go:build integration

package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func oiEvidence(t *testing.T) {
	t.Helper()
	seed, err := strconv.ParseInt(os.Getenv("SICP_OI_SEED"), 10, 64)
	if err != nil {
		t.Fatal("OI seed required")
	}
	out := os.Getenv("SICP_OI_OUTPUT")
	if out == "" {
		t.Fatal("OI artifact directory required")
	}
	started := time.Now().UTC()
	t.Cleanup(func() {
		raw, err := json.Marshal(map[string]any{"case": t.Name(), "seed": seed, "started_at": started, "finished_at": time.Now().UTC(), "failed": t.Failed()})
		if err != nil {
			t.Error(err)
			return
		}
		if err = os.WriteFile(filepath.Join(out, strings.ReplaceAll(t.Name(), "/", "_")+".json"), append(raw, '\n'), 0600); err != nil {
			t.Error(err)
		}
	})
}

type oiLogs struct {
	e        *environment
	sanitize *observability.Sanitizer
	service  [2]*logquery.Service
	query    [2]*httptest.Server
	proxy    [2]*httptest.Server
	auth     *admin.Sessions
	cookie   *http.Cookie
	csrf     string
	registry storage.Registry
	db       *adapters.Postgres
}

func newOILogs(t *testing.T) *oiLogs {
	t.Helper()
	e := newEnvironmentMode(t, 5*time.Minute, "", true)
	e.migrate(t)
	f := &oiLogs{e: e}
	var err error
	f.sanitize, err = observability.NewSanitizer(262144, 4096, []byte(strings.Repeat("s", 32)), []string{syntheticSecret})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"LOG_STORAGE_MODE": "dual"}
	for i, k := range []string{"DATABASE_M0_URL", "DATABASE_M1_URL", "DATABASE_S0_URL", "DATABASE_S1_URL", "DATABASE_CONTROL_URL", "DATABASE_LOG_URL", "DATABASE_UPSTREAM_LOG_URL"} {
		values[k] = e.dsns[i]
	}
	for i, k := range []string{"DATABASE_LOG_READ_URL", "DATABASE_UPSTREAM_LOG_READ_URL"} {
		u, _ := url.Parse(e.dsns[5+i])
		u.User = url.UserPassword("sicp_query", "synthetic")
		values[k] = u.String()
	}
	f.registry, err = storage.LoadRegistry(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	f.db, err = adapters.OpenPostgres(e.ctx, f.registry, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	signer, err := logquery.NewSigner([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	f.auth, err = admin.NewSessions("admin", "synthetic-admin-password", "query-key", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		f.service[i], err = logquery.NewService(logquery.Repository{Registry: f.registry, Database: storage.VerifiedLogs{Registry: f.registry, Database: f.db}, Sanitizer: f.sanitize}, signer, f.sanitize)
		if err != nil {
			t.Fatal(err)
		}
		handler, err := admin.LogRoutes(f.service[i], logquery.NewStreamServer(f.service[i]), "query-key", strings.Repeat("i", 32))
		if err != nil {
			t.Fatal(err)
		}
		f.query[i] = httptest.NewServer(handler)
		t.Cleanup(f.query[i].Close)
		proxy, err := admin.LogProxy(f.query[i].URL, strings.Repeat("i", 32), f.auth)
		if err != nil {
			t.Fatal(err)
		}
		mux := http.NewServeMux()
		mux.Handle("/admin/v1/logs/", proxy)
		mux.HandleFunc("POST /console-api/login", f.auth.Login)
		mux.HandleFunc("POST /console-api/logout", f.auth.Logout)
		f.proxy[i] = httptest.NewServer(mux)
		t.Cleanup(f.proxy[i].Close)
	}
	r := httptest.NewRequest("POST", "/console-api/login", strings.NewReader(`{"username":"admin","password":"synthetic-admin-password"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-SICP-Request", "console")
	w := httptest.NewRecorder()
	f.auth.Login(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	f.cookie = w.Result().Cookies()[0]
	var body struct{ Data struct{ CSRF string } }
	if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	f.csrf = body.Data.CSRF
	return f
}

func (f *oiLogs) event(t *testing.T, id string, source storage.LogSource, at time.Time) observability.SafeEvent {
	t.Helper()
	id = os.Getenv("SICP_OI_SEED") + "_" + id
	kind := events.Request
	identity := events.Identity{RequestID: id}
	if source == storage.UpstreamLog {
		kind = events.Call
		identity = events.Identity{TaskID: "synthetic-task", CallID: id}
	}
	safe, err := f.sanitize.Sanitize(events.Event{ID: id, EntityID: id, Kind: kind, Phase: events.Started, Identity: identity, OccurredAt: at}, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	return safe
}
func (f *oiLogs) append(t *testing.T, id string, source storage.LogSource) string {
	t.Helper()
	safe := f.event(t, id, source, time.Now().UTC().Truncate(time.Microsecond))
	if err := (observability.Repository{Registry: f.e.registry, Database: storage.VerifiedLogs{Registry: f.e.registry, Database: f.e.db}, Source: source}).StoreBatch(f.e.ctx, []observability.SafeEvent{safe}); err != nil {
		t.Fatal(err)
	}
	return safe.ID()
}
func (f *oiLogs) get(t *testing.T, instance int, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.e.ctx, 6*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "GET", f.proxy[instance].URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(f.cookie)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil || len(raw) > 2<<20 {
		t.Fatal("unbounded response", err)
	}
	return response, raw
}
func (f *oiLogs) page(t *testing.T, instance int, source storage.LogSource, query string) logquery.Page {
	t.Helper()
	response, raw := f.get(t, instance, "/admin/v1/logs/"+string(source)+"/events?"+query, nil)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode, string(raw))
	}
	var out struct{ Data logquery.Page }
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.Data
}

type oiFrame struct {
	Kind, ID, Data string
	Err            error
}

func readOIFrames(ctx context.Context, body io.Reader, frames chan<- oiFrame) {
	defer close(frames)
	s := bufio.NewScanner(body)
	s.Buffer(make([]byte, 4096), 512<<10)
	frame := oiFrame{}
	for s.Scan() {
		line := s.Text()
		if line == "" {
			select {
			case frames <- frame:
			case <-ctx.Done():
				return
			}
			frame = oiFrame{}
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			switch k {
			case "event":
				frame.Kind = strings.TrimSpace(v)
			case "id":
				frame.ID = strings.TrimSpace(v)
			case "data":
				frame.Data = strings.TrimSpace(v)
			}
		}
	}
	if err := s.Err(); err != nil {
		select {
		case frames <- oiFrame{Err: err}:
		case <-ctx.Done():
		}
	}
}
func (f *oiLogs) stream(t *testing.T, instance int, cursor string, last string) (<-chan oiFrame, func(), int) {
	return f.streamSource(t, instance, storage.PlatformLog, cursor, last)
}
func (f *oiLogs) streamSource(t *testing.T, instance int, source storage.LogSource, cursor string, last string) (<-chan oiFrame, func(), int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.e.ctx, 2*time.Minute)
	r, err := http.NewRequestWithContext(ctx, "GET", f.proxy[instance].URL+"/admin/v1/logs/"+string(source)+"/stream?cursor="+url.QueryEscape(cursor), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(f.cookie)
	if last != "" {
		r.Header.Set("Last-Event-ID", last)
	}
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	frames := make(chan oiFrame, 1000)
	go readOIFrames(ctx, response.Body, frames)
	closeStream := func() { cancel(); response.Body.Close() }
	t.Cleanup(closeStream)
	return frames, closeStream, response.StatusCode
}
func nextOIFrame(t *testing.T, frames <-chan oiFrame, kind string) oiFrame {
	t.Helper()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case frame, ok := <-frames:
			if !ok || frame.Err != nil {
				t.Fatal("stream ended before frame", frame.Err)
			}
			if frame.Kind == kind {
				return frame
			}
		case <-timer.C:
			t.Fatal("frame budget exceeded")
		}
	}
}
func oiActor() string { sum := sha256.Sum256([]byte("admin")); return hex.EncodeToString(sum[:]) }
