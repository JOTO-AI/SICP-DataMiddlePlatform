package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestT15_14ManagementToolUsesExplicitActionIdentityAndNoRedirects(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/console-api/login" {
			http.SetCookie(w, &http.Cookie{Name: "sicp_session", Value: "synthetic-session", Path: "/"})
			_, _ = w.Write([]byte(`{"data":{"csrf":"synthetic-csrf"}}`))
			return
		}
		if r.URL.Path == "/console-api/logout" {
			w.WriteHeader(200)
			return
		}
		calls.Add(1)
		cookie, err := r.Cookie("sicp_session")
		if r.URL.Path != "/admin/v1/tasks/initialize" || r.Method != "POST" || r.Header.Get("Idempotency-Key") != "stable-key" || r.Header.Get("X-SICP-CSRF") != "synthetic-csrf" || err != nil || cookie.Value != "synthetic-session" || r.Header.Get("Authorization") != "" {
			t.Error("incorrect admin request")
		}
		w.Header().Set("Location", "/redirect")
		w.WriteHeader(302)
		_, _ = w.Write([]byte(`{"error":"redirect"}`))
	}))
	defer s.Close()
	t.Setenv("MANAGEMENT_URL", s.URL)
	t.Setenv("ADMIN_USERNAME", "synthetic-admin")
	t.Setenv("ADMIN_PASSWORD", "synthetic-admin-password")
	if e := run([]string{"admin", "initialize", "stable-key"}); e == nil || calls.Load() != 1 {
		t.Fatal("redirect followed or successful", calls.Load(), e)
	}
	if e := run([]string{"admin", "rerun", "key", "../unexpected"}); e == nil || calls.Load() != 1 {
		t.Fatal("invalid task ID sent")
	}
}
