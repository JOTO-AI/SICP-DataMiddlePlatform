package transport_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func TestT06_01RejectCredentialsBeforeCacheAndSQL(t *testing.T) {
	for name, headers := range map[string]http.Header{
		"missing": {}, "empty": {"X-Api-Key": {""}},
		"wrong": {"X-Api-Key": {"wrong"}}, "duplicate": {"X-Api-Key": {"synthetic-key", "synthetic-key"}},
		"combined":   {"X-Api-Key": {"synthetic-key,synthetic-key"}},
		"mixed-case": {"X-Api-Key": {"synthetic-key"}, "x-api-key": {"synthetic-key"}},
		"space":      {"X-Api-Key": {" synthetic-key"}}, "oversize": {"X-Api-Key": {strings.Repeat("x", 4097)}},
		"bearer-only": {"Authorization": {"Bearer synthetic-key"}},
	} {
		t.Run(name, func(t *testing.T) {
			cacheCalls, sqlCalls := 0, 0
			h, err := transport.APIKey("synthetic-key", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cacheCalls++
				sqlCalls++
			}))
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/?api_key=synthetic-key", nil)
			r.Header = headers
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 || cacheCalls != 0 || sqlCalls != 0 {
				t.Fatalf("status=%d cache=%d sql=%d", w.Code, cacheCalls, sqlCalls)
			}
		})
	}
}

func TestT06_01ValidCredentialAndInvalidConfiguration(t *testing.T) {
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) })
	for _, key := range []string{"", " ", "key,other", "key\n", "密钥", strings.Repeat("x", 4097)} {
		if _, err := transport.APIKey(key, next); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	if _, err := transport.APIKey("synthetic-key", nil); err == nil {
		t.Fatal("nil handler accepted")
	}
	h, err := transport.APIKey("synthetic-key", next)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("X-API-Key", "synthetic-key")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if calls != 1 || w.Code != 204 {
		t.Fatal(calls, w.Code)
	}
}

func TestT06_14FailureEnvelope(t *testing.T) {
	for _, tc := range []struct {
		category contract.ErrorCategory
		status   int
		message  string
	}{
		{contract.InvalidArgument, 400, "Invalid request"}, {contract.Unauthenticated, 401, "Authentication required"},
		{contract.Forbidden, 403, "Access denied"}, {contract.NotReady, 503, "Service not ready"},
		{contract.Conflict, 409, "Snapshot conflict"}, {contract.DependencyFailure, 503, "Dependency unavailable"},
		{contract.DeadlineExceeded, 504, "Query timed out"}, {contract.Internal, 500, "Internal error"},
	} {
		t.Run(string(tc.category), func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", nil)
			r.Header.Set("X-Request-ID", "untrusted-secret")
			r = r.WithContext(transport.WithRequestID(r.Context(), "server-id"))
			w := httptest.NewRecorder()
			transport.WriteFailure(w, r, fmt.Errorf("secret wrapper: %w", &transport.Failure{Category: tc.category, Cause: errors.New("SELECT secret_password FROM private")}))
			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || len(got) != 4 || got["code"] != string(tc.category) || got["message"] != tc.message || got["request_id"] != "server-id" || got["data"] != nil {
				t.Fatal(w.Code, got)
			}
			if strings.Contains(w.Body.String(), "secret") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Body.String())
			}
		})
	}
}

func TestT06_14UnknownErrorsAndInvalidSuccessAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{errors.New("secret SQL"), 500}, {&transport.Failure{Category: "secret"}, 500},
		{fmt.Errorf("secret: %w", context.DeadlineExceeded), 504},
	} {
		w := httptest.NewRecorder()
		transport.WriteFailure(w, httptest.NewRequest("POST", "/", nil), tc.err)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	transport.WriteSuccess(w, httptest.NewRequest("POST", "/", nil), math.NaN())
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"data":null`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	transport.WriteSuccess(w, httptest.NewRequest("POST", "/", nil), map[string]int{"total": 0})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":{"total":0}`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
