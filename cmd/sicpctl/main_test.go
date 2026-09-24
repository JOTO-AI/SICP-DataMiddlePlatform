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
		calls.Add(1)
		if r.URL.Path != "/admin/v1/tasks/initialize" || r.Method != "POST" || r.Header.Get("Idempotency-Key") != "stable-key" || r.Header.Get("Authorization") != "Bearer synthetic-management-token" {
			t.Error("incorrect admin request")
		}
		w.Header().Set("Location", "/redirect")
		w.WriteHeader(302)
		_, _ = w.Write([]byte(`{"error":"redirect"}`))
	}))
	defer s.Close()
	t.Setenv("MANAGEMENT_URL", s.URL)
	t.Setenv("MANAGEMENT_TOKEN", "synthetic-management-token")
	if e := run([]string{"admin", "initialize", "stable-key"}); e == nil || calls.Load() != 1 {
		t.Fatal("redirect followed or successful", calls.Load(), e)
	}
	if e := run([]string{"admin", "rerun", "key", "../unexpected"}); e == nil || calls.Load() != 1 {
		t.Fatal("invalid task ID sent")
	}
}
