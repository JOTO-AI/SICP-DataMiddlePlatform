//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
)

func TestT33_15To17NotificationHTTPAuthorizationPagingAndSavedRevision(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	c := e.connection(t, 4)
	repo := operations.Repository{Registry: e.registry, Database: e.db}
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.EnsureNotifications(e.ctx, "", true, at); err != nil {
		t.Fatal(err)
	}
	settings, err := repo.SaveNotifications(e.ctx, operations.NotificationSettings{Revision: 1, Enabled: true, Recipients: []string{"saved@example.invalid"}, Rules: map[string]bool{"task_failed": true}}, true, "admin", at)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = c.QueryRow(e.ctx, "SELECT count(*) FROM control.notification_deliveries").Scan(&count); err != nil || count != 0 {
		t.Fatal("save sent mail", count, err)
	}
	job, err := repo.AcceptNotificationTest(e.ctx, "admin", "test-key", settings.Revision, true, at)
	if err != nil {
		t.Fatal(err)
	}
	settings.Enabled = false
	if settings, err = repo.SaveNotifications(e.ctx, settings, true, "admin", at); err != nil {
		t.Fatal(err)
	}
	replay, err := repo.AcceptNotificationTest(e.ctx, "admin", "test-key", settings.Revision-1, false, at)
	if err != nil || replay.ID != job.ID {
		t.Fatal("lost-response replay", replay, err)
	}
	if _, err = repo.AcceptNotificationTest(e.ctx, "admin", "new-key", settings.Revision-1, true, at); err != operations.ErrConflict {
		t.Fatal("old revision", err)
	}
	if _, err = repo.Job(e.ctx, job.ID, "other", "notification_test"); err != operations.ErrMissing {
		t.Fatal("foreign job", err)
	}
	// Seed durable mixed results. The API must retain unknown/rejected outcomes.
	for i, id := range []string{"a", "b", "c"} {
		if _, err = c.Exec(e.ctx, `INSERT INTO control.notification_facts(id,code,occurred_at,decision,revision) VALUES($1,'task_failed',$2,'queued',2)`, id, at); err != nil {
			t.Fatal(err)
		}
		if _, err = c.Exec(e.ctx, `INSERT INTO control.notification_deliveries(id,fact_id,rule_id,revision,recipient,status,attempts,created_at,deadline) VALUES($1,$1,'task_failed',2,'saved@example.invalid',$2,1,$3,$4)`, id, []string{"accepted", "rejected", "unknown"}[i], at, at.Add(24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err = c.Exec(e.ctx, `INSERT INTO control.notification_attempts(delivery_id,number,started_at,finished_at,outcome) VALUES($1,1,$2,$2,$3)`, id, at, []string{"accepted", "rejected", "unknown"}[i]); err != nil {
			t.Fatal(err)
		}
	}
	auth, err := admin.NewSessions("admin", "synthetic-admin-password", "query-key", false)
	if err != nil {
		t.Fatal(err)
	}
	tokens := operations.NewTokens(strings.Repeat("k", 32))
	mux := http.NewServeMux()
	if err = admin.MountNotifications(mux, auth, &operations.Notifier{Repository: repo}, tokens); err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest("POST", "/console-api/login", strings.NewReader(`{"username":"admin","password":"synthetic-admin-password"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("X-SICP-Request", "console")
	lw := httptest.NewRecorder()
	auth.Login(lw, login)
	if lw.Code != 200 {
		t.Fatal(lw.Code)
	}
	cookie := lw.Result().Cookies()[0]
	var session struct{ Data struct{ CSRF string } }
	if err = json.Unmarshal(lw.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, logged, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if logged {
			r.AddCookie(cookie)
		}
		r.Header.Set("Authorization", "Bearer query-key")
		r.Header.Set("X-API-Key", "query-key")
		r.Header.Set("X-SICP-Request", "console")
		if csrf {
			r.Header.Set(admin.CSRFHeader, session.Data.CSRF)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/admin/v1/notifications", "/admin/v1/notifications/decisions", "/admin/v1/notifications/a", "/admin/v1/notifications/tests/" + job.ID, "/admin/v1/settings/notifications"} {
		if w := request("GET", path, "", false, false); w.Code != 401 {
			t.Fatal("unauthorized", path, w.Code)
		}
	}
	for _, method := range []string{"PUT", "POST"} {
		path := "/admin/v1/settings/notifications"
		if method == "POST" {
			path = "/admin/v1/notifications/test"
		}
		if w := request(method, path, `{}`, true, false); w.Code != 403 {
			t.Fatal("csrf", w.Code)
		}
	}
	if w := request("POST", "/admin/v1/notifications/test", `{"revision":"2","idempotency_key":"temporary","recipients":["temporary@example.invalid"]}`, true, true); w.Code != 400 {
		t.Fatal("temporary recipient accepted", w.Code)
	}
	var cursor string
	for _, base := range []string{"/admin/v1/notifications", "/admin/v1/notifications/decisions"} {
		path := base + "?page_size=2"
		seen := map[string]bool{}
		for page := 0; page < 2; page++ {
			w := request("GET", path, "", true, false)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var response struct {
				Data struct {
					Items []struct{ ID string }
					Next  *string `json:"next_cursor"`
				}
			}
			if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			for _, item := range response.Data.Items {
				if seen[item.ID] {
					t.Fatal("duplicate", item.ID)
				}
				seen[item.ID] = true
			}
			if page == 0 {
				if response.Data.Next == nil || len(response.Data.Items) != 2 {
					t.Fatal("first page")
				}
				cursor = *response.Data.Next
				path = base + "?cursor=" + url.QueryEscape(cursor)
			} else if response.Data.Next != nil || len(response.Data.Items) != 1 {
				t.Fatal("last page")
			}
		}
		if len(seen) != 3 {
			t.Fatal(seen)
		}
	}
	if w := request("GET", "/admin/v1/notifications?cursor="+url.QueryEscape(cursor), "", true, false); w.Code != 400 {
		t.Fatal("cross-scope cursor", w.Code)
	}
	foreign, err := tokens.Encode(operations.Token{Scope: "notifications", Subject: "other", Position: "a", Size: 2, At: at, Expires: at.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/admin/v1/notifications?cursor="+url.QueryEscape(foreign), "", true, false); w.Code != 403 {
		t.Fatal("foreign cursor", w.Code)
	}
	w := request("GET", "/admin/v1/notifications/c", "", true, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"outcome":"unknown"`) || strings.Contains(w.Body.String(), `"owner"`) {
		t.Fatal("detail lost or leaked", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?page_size=201", "?page_size=2&page_size=2", "?cursor=bad", "?recipient=other"} {
		if w := request("GET", "/admin/v1/notifications"+suffix, "", true, false); w.Code != 400 {
			t.Fatal(suffix, w.Code)
		}
	}
}
