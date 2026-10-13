package admin

import (
	"context"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestT34_11MaintenanceRoutesRejectBusinessCredentialsCSRFAndOverrides(t *testing.T) {
	auth, err := NewSessions("admin", "synthetic-admin-password", "query-key", false)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	calls := 0
	if err = MountMaintenance(mux, auth, operations.Maintenance{}, func(context.Context) map[string]any { calls++; return map[string]any{} }); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginSession(t, auth)
	request := func(method, path, body string, logged bool, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if logged {
			r.AddCookie(cookie)
		}
		r.Header.Set("Authorization", "Bearer query-key")
		r.Header.Set("X-API-Key", "query-key")
		r.Header.Set("X-SICP-Request", "console")
		r.Header.Set(CSRFHeader, token)
		r.Header.Set("X-SICP-Actor", "forged")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/admin/v1/system/status", "/admin/v1/settings/retention", "/admin/v1/maintenance/cleanup/job"} {
		if w := request("GET", path, "", false, ""); w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
	for _, tc := range []struct{ method, path string }{{"PUT", "/admin/v1/settings/retention"}, {"POST", "/admin/v1/maintenance/cleanup/preview"}, {"POST", "/admin/v1/maintenance/cleanup"}} {
		for _, token := range []string{"", "wrong"} {
			if w := request(tc.method, tc.path, `{}`, true, token); w.Code != 403 {
				t.Fatal(tc, w.Code)
			}
		}
		if w := request(tc.method, tc.path, `{"operator":"forged","path":"/data","sql":"DELETE"}`, true, csrf); w.Code != 400 {
			t.Fatal("client overrides", tc, w.Code)
		}
	}
	w := request("GET", "/admin/v1/system/status", "", true, "")
	if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), `"retention":null`) || !strings.Contains(w.Body.String(), `"runtime_fields"`) || !strings.Contains(w.Body.String(), `"deployment_fields"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request("GET", "/admin/v1/system/status?token=forged", "", true, ""); w.Code != 400 || calls != 1 {
		t.Fatal("query override")
	}
}
