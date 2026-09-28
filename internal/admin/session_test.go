package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func loginSession(t *testing.T, s *Sessions) (*http.Cookie, string) {
	t.Helper()
	r := httptest.NewRequest("POST", "/console-api/login", strings.NewReader(`{"username":"admin","password":"synthetic-admin-password"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-SICP-Request", "console")
	w := httptest.NewRecorder()
	s.Login(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body struct {
		Data struct{ CSRF, Operator string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Data.Operator != "admin" || len(body.Data.CSRF) != 43 {
		t.Fatal("invalid login receipt", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" || cookies[0].MaxAge != 28800 {
		t.Fatal("invalid session cookie", cookies)
	}
	if strings.Contains(w.Body.String(), cookies[0].Value) {
		t.Fatal("session identifier exposed to JavaScript")
	}
	return cookies[0], body.Data.CSRF
}

func TestSessionLoginAuthorizationLogoutAndExpiry(t *testing.T) {
	s, err := NewSessions("admin", "synthetic-admin-password", "query-secret", true)
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginSession(t, s)
	if !cookie.Secure {
		t.Fatal("HTTPS cookie not secure")
	}
	calls := 0
	h, err := s.Protect(ForceFull, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operator, action, ok := Identity(r.Context())
		if !ok || operator != "admin" || action != ForceFull {
			t.Error("missing audit identity")
		}
		calls++
		w.WriteHeader(202)
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		cookie     bool
		csrf, site string
		status     int
	}{
		{"no-session", false, csrf, "", 401}, {"missing-csrf", true, "", "", 403}, {"wrong-csrf", true, "wrong", "", 403}, {"cross-site", true, csrf, "cross-site", 403}, {"valid", true, csrf, "same-origin", 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/admin/v1/tasks/initialize", nil)
			if tc.cookie {
				r.AddCookie(cookie)
			}
			r.Header.Set("Authorization", "Bearer query-secret")
			r.Header.Set("X-SICP-Request", "console")
			r.Header.Set(CSRFHeader, tc.csrf)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	if calls != 1 {
		t.Fatal("unauthorized handler executed")
	}
	r := httptest.NewRequest("GET", "/console-api/session", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Current(w, r)
	if w.Code != 200 {
		t.Fatal("refresh lost session")
	}
	r = httptest.NewRequest("POST", "/console-api/logout", nil)
	r.AddCookie(cookie)
	r.Header.Set("X-SICP-Request", "console")
	r.Header.Set(CSRFHeader, csrf)
	w = httptest.NewRecorder()
	s.Logout(w, r)
	if w.Code != 200 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout failed")
	}
	r = httptest.NewRequest("GET", "/console-api/session", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	s.Current(w, r)
	if w.Code != 401 {
		t.Fatal("revoked session accepted")
	}
	cookie, _ = loginSession(t, s)
	s.now = func() time.Time { return time.Now().Add(9 * time.Hour) }
	r = httptest.NewRequest("GET", "/console-api/session", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	s.Current(w, r)
	if w.Code != 401 {
		t.Fatal("expired session accepted")
	}
}

func TestLoginRejectsCrossSiteAndLimitsAttempts(t *testing.T) {
	s, err := NewSessions("admin", "synthetic-admin-password", "query", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		r := httptest.NewRequest("POST", "/console-api/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		want := 401
		if i == 0 {
			want = 403
		} else {
			r.Header.Set("X-SICP-Request", "console")
		}
		if i == 11 {
			want = 429
		}
		w := httptest.NewRecorder()
		s.Login(w, r)
		if w.Code != want || len(w.Result().Cookies()) != 0 {
			t.Fatal(i, w.Code)
		}
	}
}

func TestConsoleProxyKeepsSecretsServerSideAndRejectsRedirects(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-API-Key") != "server-key" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get(CSRFHeader) != "" || r.Header.Get("X-Forwarded-Host") != "" {
			t.Error("incorrect upstream credentials")
		}
		if r.URL.Path == "/readyz" {
			w.Header().Set("Location", "https://other.invalid/")
			w.WriteHeader(302)
			return
		}
		w.Header().Set("Set-Cookie", "upstream=secret")
		w.Header().Set("X-Query-Snapshot", "snapshot")
		_, _ = w.Write([]byte(`{"code":"ok"}`))
	}))
	defer upstream.Close()
	h, err := QueryProxy(upstream.URL, "server-key")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/console-api/query/api/v1/sicp/students/grades/query", 200}, {"GET", "/console-api/query/readyz", 502}, {"POST", "/console-api/query/admin/v1/tasks/initialize", 404},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		r.Header.Set("Cookie", "sicp_session=browser")
		r.Header.Set("Authorization", "Bearer browser")
		r.Header.Set(CSRFHeader, "browser")
		r.Header.Set("X-API-Key", "browser")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Set-Cookie") != "" {
			t.Fatal(w.Code, w.Header())
		}
	}
	if calls != 2 {
		t.Fatal("unapproved route reached upstream")
	}
}
