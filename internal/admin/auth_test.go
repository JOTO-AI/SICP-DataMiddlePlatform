package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
)

const adminToken = "synthetic-administrator-secret"
const queryToken = "synthetic-business-query-secret"

func administrator(t *testing.T, actions ...admin.Action) *admin.Auth {
	t.Helper()
	a, err := admin.NewAuth(queryToken, []admin.Credential{{Operator: "operator-001", Token: adminToken, Actions: actions}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestT15_01InvalidOrBusinessCredentialsNeverReachManagement(t *testing.T) {
	for name, headers := range map[string]http.Header{
		"missing": {}, "business-key": {"X-Api-Key": {queryToken}}, "business-bearer": {"Authorization": {"Bearer " + queryToken}}, "duplicate": {"Authorization": {"Bearer " + adminToken, "Bearer " + adminToken}}, "mixed-case": {"Authorization": {"Bearer " + adminToken}, "authorization": {"Bearer " + adminToken}}, "combined": {"Authorization": {"Bearer " + adminToken + ",Bearer " + adminToken}}, "space": {"Authorization": {"Bearer  " + adminToken}}, "wrong": {"Authorization": {"Bearer synthetic-wrong-secret"}}, "oversized": {"Authorization": {"Bearer " + strings.Repeat("a", 4097)}},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ })
			h, err := administrator(t, admin.ForceFull).Protect(admin.ForceFull, next)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/admin/v1/tasks/initialize?token="+adminToken, nil)
			req.Header = headers
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != 401 || calls != 0 || strings.Contains(w.Body.String(), adminToken) || strings.Contains(w.Body.String(), queryToken) {
				t.Fatal(w.Code, calls, w.Body.String())
			}
		})
	}
}

func TestT15_01And02EachRouteRequiresItsOwnPermissionAndTrustedOperator(t *testing.T) {
	routes := []struct {
		action       admin.Action
		method, path string
	}{{admin.ForceFull, "POST", "/admin/v1/tasks/force-full"}, {admin.Rerun, "POST", "/admin/v1/tasks/failed-task/rerun"}, {admin.Status, "GET", "/admin/v1/tasks/task-123"}}
	for _, allowed := range routes {
		t.Run(string(allowed.action), func(t *testing.T) {
			calls := 0
			handlers := map[admin.Action]http.Handler{}
			for _, route := range routes {
				handlers[route.action] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					operator, action, ok := admin.Identity(r.Context())
					if !ok || operator != "operator-001" || action != route.action {
						t.Fatal(operator, action, ok)
					}
					if action == admin.Rerun && r.PathValue("task_id") != "failed-task" {
						t.Fatal("lost task identity")
					}
					w.WriteHeader(204)
				})
			}
			mux, err := admin.Routes(administrator(t, allowed.action), handlers)
			if err != nil {
				t.Fatal(err)
			}
			for _, route := range routes {
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"requested_by":"forged"}`))
				req.Header.Set("Authorization", "Bearer "+adminToken)
				req.Header.Set("X-Operator", "forged")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				want := 403
				if route.action == allowed.action {
					want = 204
				}
				if w.Code != want {
					t.Fatal(route, w.Code)
				}
			}
			if calls != 1 {
				t.Fatal("unauthorized action reached task backend", calls)
			}
		})
	}
}

func TestT15_01CredentialConfigurationRejectsSharedSecretsAndCopiesPermissions(t *testing.T) {
	for _, credential := range []admin.Credential{{Operator: "admin", Token: queryToken, Actions: []admin.Action{admin.Status}}, {Operator: "", Token: adminToken, Actions: []admin.Action{admin.Status}}, {Operator: "admin", Token: "short", Actions: []admin.Action{admin.Status}}, {Operator: "admin", Token: adminToken, Actions: []admin.Action{"unknown"}}} {
		if _, err := admin.NewAuth(queryToken, []admin.Credential{credential}); err == nil {
			t.Fatal("invalid management configuration")
		}
	}
	credentials := []admin.Credential{{Operator: "operator-001", Token: adminToken, Actions: []admin.Action{admin.Status}}}
	a, err := admin.NewAuth(queryToken, credentials)
	if err != nil {
		t.Fatal(err)
	}
	credentials[0].Actions[0] = admin.ForceFull
	h, err := a.Protect(admin.ForceFull, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("permissions mutated after setup") }))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
