package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
)

type logReader struct {
	calls int
	err   error
}

func (r *logReader) Read(context.Context, logquery.Source, logquery.ReadRequest) (logquery.Batch, error) {
	r.calls++
	return logquery.Batch{Snapshot: logquery.Snapshot{Epoch: "epoch-1", HighWater: 0, Floor: 0}}, r.err
}
func TestT29_11InternalAuthorizationBeforeReadAndNoBusinessKey(t *testing.T) {
	reader := &logReader{}
	signer, _ := logquery.NewSigner([]byte(strings.Repeat("c", 32)))
	sanitizer, _ := observability.NewSanitizer(16384, 2048, []byte(strings.Repeat("a", 32)), nil)
	svc, _ := logquery.NewService(reader, signer, sanitizer)
	h, err := LogRoutes(svc, nil, "business-key-123456789", strings.Repeat("x", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "business-key-123456789", strings.Repeat("z", 32)} {
		req := httptest.NewRequest("GET", "/internal/admin/v1/logs/platform/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-SICP-Actor", strings.Repeat("a", 64))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 401 || reader.calls != 0 {
			t.Fatal("unauthorized query")
		}
	}
	req := httptest.NewRequest("GET", "/internal/admin/v1/logs/platform/events", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
	req.Header.Set("X-SICP-Actor", strings.Repeat("a", 64))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || reader.calls != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	reader.err = logquery.ErrUnavailable
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 503 || body["data"] != nil {
		t.Fatal("failure masquerades as empty")
	}
}
func TestT29_12ProxyRejectsDestinationInjectionRedirectAndOversize(t *testing.T) {
	for _, address := range []string{"http://a.invalid/path", "http://a.invalid?x=y", "http://u:p@a.invalid", "file:///tmp/x"} {
		if _, e := LogProxy(address, strings.Repeat("x", 32), &Sessions{}); e == nil {
			t.Fatal("arbitrary target accepted")
		}
	}
	// Authentication is still required on even malformed paths/queries.
	s := &Sessions{operator: "admin", sessions: map[[32]byte]session{}, now: time.Now}
	h, e := LogProxy("http://fixed.invalid", strings.Repeat("x", 32), s)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/admin/v1/logs/platform/events?url=http://evil.invalid", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	cookie := strings.Repeat("t", 43)
	s.sessions[sha256.Sum256([]byte(cookie))] = session{expires: time.Now().Add(time.Hour)}
	for _, mode := range []string{"redirect", "large", "ok"} {
		t.Run(mode, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) || r.Header.Get("X-Evil") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-SICP-Actor") == strings.Repeat("f", 64) {
					t.Error("proxy header injection")
				}
				w.Header().Set("Content-Type", "application/json")
				switch mode {
				case "redirect":
					w.Header().Set("Location", "http://evil.invalid")
					w.WriteHeader(302)
				case "large":
					_, _ = w.Write([]byte(strings.Repeat("x", logquery.MaxResponseBytes+1)))
				case "ok":
					_, _ = w.Write([]byte(`{"code":"ok","message":"","data":{"items":[]}}`))
				}
			}))
			defer up.Close()
			h, e := LogProxy(up.URL, strings.Repeat("x", 32), s)
			if e != nil {
				t.Fatal(e)
			}
			req := httptest.NewRequest("GET", "/admin/v1/logs/platform/events", nil)
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
			req.Header.Set("X-Evil", "arbitrary")
			req.Header.Set("X-SICP-Actor", strings.Repeat("f", 64))
			out := httptest.NewRecorder()
			h.ServeHTTP(out, req)
			want := 502
			if mode == "ok" {
				want = 200
			}
			if out.Code != want {
				t.Fatal(out.Code, out.Body.String())
			}
		})
	}
}
