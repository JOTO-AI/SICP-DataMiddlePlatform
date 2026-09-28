package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
)

func TestHealthcheckUsesConfiguredListenerAndLiveness(t *testing.T) {
	for _, role := range []config.Role{config.Query, config.Worker} {
		t.Run(string(role), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/livez" {
					t.Errorf("probe used %s instead of liveness", r.URL.Path)
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()
			t.Setenv("HTTP_ADDRESS", strings.TrimPrefix(server.URL, "http://"))
			// A deployment probe needs neither full runtime configuration nor
			// access to environment-configured HTTP proxies.
			t.Setenv("QUERY_API_KEY", "")
			t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
			if code := Entrypoint(role, []string{"--healthcheck"}); code != 0 || calls.Load() != 1 {
				t.Fatalf("exit=%d probe calls=%d", code, calls.Load())
			}
		})
	}
}

func TestHealthcheckRejectsUnhealthyAndRedirectResponses(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/livez" {
					t.Error("healthcheck followed redirect")
					return
				}
				w.Header().Set("Location", "/other")
				w.WriteHeader(status)
			}))
			defer server.Close()
			t.Setenv("HTTP_ADDRESS", strings.TrimPrefix(server.URL, "http://"))
			if code := Entrypoint(config.Worker, []string{"--healthcheck"}); code != 1 {
				t.Fatalf("unhealthy probe exit=%d", code)
			}
		})
	}
}

func TestHealthcheckAddress(t *testing.T) {
	for _, tc := range []struct {
		address, want string
	}{
		{"0.0.0.0:9081", "http://127.0.0.1:9081/livez"},
		{":9081", "http://127.0.0.1:9081/livez"},
		{"[::]:9081", "http://[::1]:9081/livez"},
		{"[::ffff:0.0.0.0]:9081", "http://127.0.0.1:9081/livez"},
		{"[fe80::1%eth0]:9081", "http://[fe80::1%25eth0]:9081/livez"},
		{"127.0.0.2:9081", "http://127.0.0.2:9081/livez"},
		{"", ""}, {"localhost", ""}, {"localhost:0", ""}, {"localhost:65536", ""},
	} {
		t.Run(tc.address, func(t *testing.T) {
			got, err := healthcheckURL(config.Worker, func(string) (string, bool) { return tc.address, true })
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("got=%q err=%v want=%q", got, err, tc.want)
			}
		})
	}
	for role, want := range map[config.Role]string{config.Query: "http://127.0.0.1:8080/livez", config.Worker: "http://127.0.0.1:8081/livez"} {
		got, err := healthcheckURL(role, func(string) (string, bool) { return "", false })
		if got != want || err != nil {
			t.Fatalf("default %s: %q %v", role, got, err)
		}
	}
}
