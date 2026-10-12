package admin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
)

func TestT27_LogsReadRequiresExplicitIndependentGrant(t *testing.T) {
	for _, tc := range []struct {
		name  string
		grant admin.Action
		token string
		want  int
	}{
		{"status-does-not-grant-logs", admin.Status, adminToken, 403},
		{"business-key-does-not-grant-logs", admin.LogsRead, queryToken, 401},
		{"logs-grant", admin.LogsRead, adminToken, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h, err := administrator(t, tc.grant).Protect(admin.LogsRead, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				operator, action, ok := admin.Identity(r.Context())
				if !ok || operator != "operator-001" || action != admin.LogsRead {
					t.Fatal("missing trusted log identity")
				}
				calls++
				w.WriteHeader(204)
			}))
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "/admin/v1/logs/platform/events", nil)
			r.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || (calls == 1) != (tc.want == 204) {
				t.Fatal("unexpected log authorization result", w.Code, calls)
			}
		})
	}
}
