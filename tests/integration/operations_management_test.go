//go:build integration && linux

package integration

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Existing end-to-end fixtures execute real workers, PostgreSQL, Redis and the
// synthetic upstream. Reuse their assertions instead of manufacturing facts.
func TestOI04_01(t *testing.T) {
	oiEvidence(t)
	t.Run("full_and_publication", TestI04_03)
	t.Run("reader_drain", TestI04_04)
}
func TestOI04_02(t *testing.T) {
	oiEvidence(t)
	t.Run("unchanged", TestI04_09)
	t.Run("second_copy_failure", TestI04_05)
}
func TestOI04_03(t *testing.T) {
	oiEvidence(t)
	t.Run("recover_original", TestI05_01)
	t.Run("explicit_rerun", TestI06_05)
}
func TestOI04_04(t *testing.T) {
	oiEvidence(t)
	t.Run("concurrent_admission", TestI06_03)
	t.Run("single_table", TestI06_07)
	t.Run("disconnected_client", TestI06_04)
}

type oiConsole struct {
	server *httptest.Server
	cookie *http.Cookie
	csrf   string
	ctx    context.Context
}

func newOIConsole(t *testing.T, f *runtimeFixture) *oiConsole {
	t.Helper()
	server := httptest.NewServer(f.worker.Management)
	t.Cleanup(server.Close)
	c := &oiConsole{server: server, ctx: f.e.ctx}
	raw, _ := json.Marshal(map[string]string{"username": f.wc.AdminUsername, "password": f.wc.AdminPassword})
	r, _ := http.NewRequestWithContext(f.e.ctx, "POST", server.URL+"/console-api/login", strings.NewReader(string(raw)))
	r.Header.Set("X-SICP-Request", "console")
	r.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("console login", response.StatusCode)
	}
	c.cookie = response.Cookies()[0]
	var out struct{ Data struct{ CSRF string } }
	if err = json.NewDecoder(response.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	c.csrf = out.Data.CSRF
	return c
}
func (c *oiConsole) request(t *testing.T, method, path, body string, authorized bool) (int, json.RawMessage) {
	t.Helper()
	r, _ := http.NewRequestWithContext(c.ctx, method, c.server.URL+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-SICP-Request", "console")
	if authorized {
		r.AddCookie(c.cookie)
		r.Header.Set("X-SICP-CSRF", c.csrf)
	} else {
		r.Header.Set("X-API-Key", "synthetic-query-secret")
	}
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		t.Fatal("response bound", err)
	}
	return response.StatusCode, raw
}

func TestOI05_01(t *testing.T) {
	oiEvidence(t)
	f := newRuntime(t, true)
	c := newOIConsole(t, f)
	status, raw := c.request(t, "GET", "/admin/v1/apis", "", true)
	var catalog struct {
		Data struct {
			Version string
			Items   []operations.API
		}
	}
	if status != 200 || json.Unmarshal(raw, &catalog) != nil || len(catalog.Data.Items) != 23 || catalog.Data.Version != operations.CatalogVersion {
		t.Fatal("catalog contract", status, string(raw))
	}
	for _, entry := range catalog.Data.Items {
		status, raw = c.request(t, "GET", "/admin/v1/apis/"+entry.ID, "", true)
		if status != 200 {
			t.Fatal(entry.ID, status, string(raw))
		}
	}
	status, _ = c.request(t, "GET", "/admin/v1/apis/undefined", "", true)
	if status != 404 {
		t.Fatal("undefined API enabled", status)
	}
}
func TestOI05_02(t *testing.T) {
	oiEvidence(t)
	t.Run("all_nine_business_contracts", TestI07_Phase2RealHTTPAssociations)
	t.Run("operation_snapshots_and_atomic_8_9", TestI08_Phase2PaginationAndAtomicClient)
	t.Run("actual_console_proxy", oiNineProxy)
}

func oiNineProxy(t *testing.T) {
	f := phase2EdgeRuntime(t, nil)
	proxy, err := admin.QueryProxy(f.server.URL, f.qc.Service.APIKey)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := admin.NewSessions("admin", "synthetic-admin-password", "query-key", false)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(auth.Require(proxy))
	t.Cleanup(server.Close)
	login := httptest.NewRequest("POST", "/console-api/login", strings.NewReader(`{"username":"admin","password":"synthetic-admin-password"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("X-SICP-Request", "console")
	w := httptest.NewRecorder()
	auth.Login(w, login)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	cookie := w.Result().Cookies()[0]
	var loginBody struct{ Data struct{ CSRF string } }
	if json.Unmarshal(w.Body.Bytes(), &loginBody) != nil {
		t.Fatal("login response")
	}
	call := func(op contract.Operation, body, token string) (http.Header, map[string]any) {
		t.Helper()
		path := ""
		for _, definition := range contract.Definitions() {
			if definition.Operation == op {
				path = definition.Path
			}
		}
		r, _ := http.NewRequestWithContext(f.e.ctx, "POST", server.URL+path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("X-SICP-Request", "console")
		r.Header.Set("X-SICP-CSRF", loginBody.Data.CSRF)
		r.Header.Set("X-Query-Snapshot", token)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var out struct{ Data map[string]any }
		if json.NewDecoder(response.Body).Decode(&out) != nil || response.StatusCode != 200 || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("business proxy", op, response.StatusCode)
		}
		return response.Header, out.Data
	}
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{
		{contract.StudentGrades, `{"student_number":"S001","academic_year":"all"}`}, {contract.BatchGrades, `{"student_numbers":["S001"],"academic_year":"all"}`},
		{contract.StudentTimetable, `{"student_number":"NO_SUCH_STUDENT","academic_year":"2026-2027","semester":"1"}`}, {contract.StaffTimetable, `{"staff_number":"NO_SUCH_TEACHER","academic_year":"2026-2027","semester":"1"}`},
		{contract.Organizations, `{"page":1,"page_size":1}`}, {contract.Students, `{"student_number":"S001"}`}, {contract.Staff, `{"staff_number":"T001"}`},
	} {
		call(tc.op, tc.body, "")
	}
	classHeaders, classes := call(contract.StaffTeachingClasses, phase2Teacher, "")
	expected := phase2Item(t, classes, 0)["student_count"].(float64)
	token := ""
	members := map[string]bool{}
	for _, page := range []string{"1", "2", "3"} {
		headers, data := call(contract.TeachingClassStudents, strings.Replace(phase2Roster, `"page":1`, `"page":`+page, 1), token)
		if token != "" && token != headers.Get("X-Query-Snapshot") {
			t.Fatal("operation snapshot changed")
		}
		token = headers.Get("X-Query-Snapshot")
		if headers.Get("X-SICP-Combination") == "" || headers.Get("X-SICP-Combination") != classHeaders.Get("X-SICP-Combination") {
			t.Fatal("8-9 version changed")
		}
		id := phase2Item(t, data, 0)["student_number"].(string)
		if members[id] {
			t.Fatal("duplicate membership")
		}
		members[id] = true
	}
	if float64(len(members)) != expected {
		t.Fatal("8-9 membership count mismatch", len(members), expected)
	}
}
func TestOI05_03(t *testing.T) {
	oiEvidence(t)
	f := newRuntime(t, true)
	c := newOIConsole(t, f)
	before := countControl(t, f, "SELECT count(*) FROM control.tasks")
	requests := sourceRequests(f)
	for _, table := range source.Catalog() {
		body := fmt.Sprintf(`{"api_id":%q,"version":%q,"page":1,"page_size":1,"idempotency_key":%q}`, table.Name, operations.CatalogVersion, "test-"+table.Name)
		status, raw := c.request(t, "POST", "/admin/v1/api-tests", body, true)
		var accepted struct{ Data operations.Job }
		if status != 202 || json.Unmarshal(raw, &accepted) != nil {
			t.Fatal(table.Name, status, string(raw))
		}
		if err := f.worker.SourceTests.Pump(f.e.ctx); err != nil {
			t.Fatal(err)
		}
		status, raw = c.request(t, "GET", "/admin/v1/api-tests/"+accepted.Data.ID, "", true)
		if status != 200 || !strings.Contains(string(raw), `"status":"succeeded"`) || !strings.Contains(string(raw), `"result_state":"available"`) {
			t.Fatal(table.Name, status, string(raw))
		}
	}
	if sourceRequests(f)-requests != 14 || countControl(t, f, "SELECT count(*) FROM control.tasks") != before {
		t.Fatal("source test auto-paged or mutated sync tasks")
	}
	until := time.Now().Add(time.Minute)
	if err := f.wr.Control.ExtendCooldown(f.e.ctx, until); err != nil {
		t.Fatal(err)
	}
	status, _ := c.request(t, "POST", "/admin/v1/api-tests", fmt.Sprintf(`{"api_id":"dwd_bzkscjzlb","version":%q,"idempotency_key":"cooldown"}`, operations.CatalogVersion), true)
	if status != 429 {
		t.Fatal("source cooldown bypass", status)
	}
	if _, err := f.e.connection(t, 4).Exec(f.e.ctx, "UPDATE control.schedule SET cooldown_until=NULL"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"redirect", "oversized", "timeout", "http_failure"} {
		t.Run(mode, func(t *testing.T) {
			var calls, escaped atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { escaped.Add(1) }))
			defer target.Close()
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/apizt/openapi/invoke/sjzc/release/dwd_bzkscjzlb" {
					t.Error("unfixed source path")
				}
				switch mode {
				case "redirect":
					http.Redirect(w, r, target.URL, 302)
				case "oversized":
					w.Write([]byte(strings.Repeat("x", (2<<20)+1)))
				case "timeout":
					<-r.Context().Done()
				case "http_failure":
					http.Error(w, "synthetic-private-body", 503)
				}
			}))
			defer mock.Close()
			client, err := ingest.NewClient(mock.URL, "synthetic-secret", http.DefaultTransport, 2<<20)
			if err != nil {
				t.Fatal(err)
			}
			limiter, err := ingest.NewLimiter(1)
			if err != nil {
				t.Fatal(err)
			}
			collector, err := ingest.NewCoordinator(client, ingest.BudgetConfig{RequestTimeout: 100 * time.Millisecond, PageTimeout: time.Second, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: 1}, ingest.SystemTimer{}, testkit.FixedRandom(0), f.wr.Control, limiter.LowPriority())
			if err != nil {
				t.Fatal(err)
			}
			runner := &operations.TestRunner{Repository: operations.Repository{Registry: f.e.registry, Database: f.e.db}, Collector: collector, Recorder: f.worker.SourceTests.Recorder, Results: operations.NewResults(), Owner: "adverse"}
			request := operations.TestRequest{APIID: "dwd_bzkscjzlb", Version: operations.CatalogVersion, Page: 1, PageSize: 1}
			j, err := runner.Repository.Accept(f.e.ctx, "admin", "source_test", mode, request, time.Now(), 30*time.Second, operations.CheckSourceTest)
			if err != nil {
				t.Fatal(err)
			}
			if err = runner.Pump(f.e.ctx); err != nil {
				t.Fatal(err)
			}
			done, err := runner.Repository.Job(f.e.ctx, j.ID, "admin", "source_test")
			if err != nil || done.Status != "failed" || calls.Load() != 1 || escaped.Load() != 0 || strings.Contains(string(done.Summary), "synthetic-private-body") {
				t.Fatal("source boundary", mode, done, calls.Load(), escaped.Load(), err)
			}
		})
	}
}
func TestOI05_04(t *testing.T) {
	oiEvidence(t)
	f := newRuntime(t, true)
	c := newOIConsole(t, f)
	for _, path := range []string{"/admin/v1/apis", "/admin/v1/api-tests/foreign", "/admin/v1/settings/notifications", "/admin/v1/system/status"} {
		status, _ := c.request(t, "GET", path, "", false)
		if status != 401 {
			t.Fatal("business key elevated", path, status)
		}
	}
	for _, extra := range []string{`"url":"http://attacker.invalid"`, `"headers":{"Authorization":"secret"}`, `"operator":"other"`, `"environment":"production"`} {
		status, _ := c.request(t, "POST", "/admin/v1/api-tests", fmt.Sprintf(`{"api_id":"dwd_bzkscjzlb","version":%q,"idempotency_key":"bad",%s}`, operations.CatalogVersion, extra), true)
		if status != 400 {
			t.Fatal("injection accepted", status)
		}
	}
	if countControl(t, f, "SELECT count(*) FROM control.management_jobs") != 0 {
		t.Fatal("rejected request persisted")
	}
}
func TestOI05_05(t *testing.T) {
	oiEvidence(t)
	f := newRuntime(t, true)
	repo := operations.Repository{Registry: f.e.registry, Database: f.e.db}
	now := time.Now().UTC()
	request := operations.TestRequest{APIID: "dwd_bzkscjzlb", Version: operations.CatalogVersion, Page: 1, PageSize: 1}
	job, err := repo.Accept(f.e.ctx, "admin", "source_test", "restart", request, now, 30*time.Second, operations.CheckSourceTest)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := repo.Accept(f.e.ctx, "admin", "source_test", "restart", request, now, 30*time.Second, operations.CheckSourceTest)
	if err != nil || replay.ID != job.ID {
		t.Fatal("lost response duplicated", err)
	}
	if _, err = repo.Job(f.e.ctx, job.ID, "other", "source_test"); err != operations.ErrMissing {
		t.Fatal("foreign result", err)
	}
	claimed, err := repo.Claim(f.e.ctx, "source_test", "lost-worker", now)
	if err != nil || claimed == nil {
		t.Fatal(err)
	}
	if _, err = f.e.connection(t, 4).Exec(f.e.ctx, "UPDATE control.management_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID); err != nil {
		t.Fatal(err)
	}
	before := sourceRequests(f)
	if err = f.worker.SourceTests.Pump(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	done, err := repo.Job(f.e.ctx, job.ID, "admin", "source_test")
	if err != nil || done.Status != "interrupted" || sourceRequests(f) != before {
		t.Fatal("running job resent", done, err)
	}
	results := operations.NewResults()
	if !results.Put("result", "admin", []byte(`{"synthetic_person":"private"}`), now) {
		t.Fatal("result insert")
	}
	if _, ok := results.Get("result", "other", now); ok {
		t.Fatal("foreign payload")
	}
	if _, ok := results.Get("result", "admin", now.Add(10*time.Minute)); ok {
		t.Fatal("expired payload")
	}
	if _, ok := operations.NewResults().Get("result", "admin", now); ok {
		t.Fatal("restart retained raw payload")
	}
	if strings.Contains(string(done.Summary), "synthetic_person") || strings.Contains(string(done.Request), "private") {
		t.Fatal("raw personnel persisted")
	}
}
func TestOI06_01(t *testing.T) {
	oiEvidence(t)
	f := newRuntimeOptions(t, true, 3*time.Minute, func(v map[string]string) {
		for _, k := range []string{"MAIL_ADDRESS", "MAIL_TLS", "MAIL_FROM", "MAIL_TO", "MAIL_USERNAME", "MAIL_PASSWORD"} {
			delete(v, k)
		}
	})
	f.queryOK(t, contract.StudentGrades, allGrades, "")
	if f.worker.Notifier == nil || f.worker.Notifier.Channel != nil {
		t.Fatal("optional SMTP assembly")
	}
	if err := f.worker.Notifier.Pump(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	if countControl(t, f, "SELECT count(*) FROM control.notification_deliveries") != 0 {
		t.Fatal("unconfigured mail created sends")
	}
}
func TestOI06_02(t *testing.T) {
	oiEvidence(t)
	t.Run("save_revision_history", TestT33_05PostgresLegacyConfigurationAndHistoryMatrix)
	t.Run("saved_revision_http", TestT33_15To17NotificationHTTPAuthorizationPagingAndSavedRevision)
	t.Run("empty_recipient_atomic_disable", func(t *testing.T) {
		e := newEnvironment(t)
		e.migrate(t)
		r := operations.Repository{Registry: e.registry, Database: e.db}
		now := time.Now()
		if err := r.EnsureNotifications(e.ctx, "old@example.invalid", true, now); err != nil {
			t.Fatal(err)
		}
		s, err := r.NotificationSettings(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		revision := s.Revision
		s.Recipients = []string{}
		s, err = r.SaveNotifications(e.ctx, s, true, "admin", now)
		if err != nil || s.Enabled || len(s.Recipients) != 0 || s.Revision != revision+1 {
			t.Fatal("empty recipient update not atomic", s, err)
		}
	})
}
func TestOI06_03(t *testing.T) {
	oiEvidence(t)
	t.Run("partial_receipts_unknown_budgets", TestT33_06To11PostgresClaimCloseRecoveryAndBudgets)
	t.Run("actual_notifier_channel_receipts", oiMailReceipts)
}

type oiMail func(context.Context, notification.Mail) (notification.Receipt, error)

func (f oiMail) Send(ctx context.Context, m notification.Mail) (notification.Receipt, error) {
	return f(ctx, m)
}
func oiMailReceipts(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := operations.Repository{Registry: e.registry, Database: e.db}
	now := time.Now()
	c := e.connection(t, 4)
	if err := r.EnsureNotifications(e.ctx, "", true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SaveNotifications(e.ctx, operations.NotificationSettings{Revision: 1, Enabled: true, Recipients: []string{"accepted@example.invalid", "rejected@example.invalid", "unknown@example.invalid", "retry@example.invalid"}, Rules: map[string]bool{"task_failed": true}}, true, "admin", now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(e.ctx, `INSERT INTO control.tasks(id,idempotency_key,mode,status,requested_by,accepted_at,finished_at) VALUES('mail','mail','initialize','failed','synthetic',$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	n := operations.Notifier{Repository: r, Owner: "mail", Channel: oiMail(func(_ context.Context, m notification.Mail) (notification.Receipt, error) {
		counts[m.To]++
		switch m.To {
		case "accepted@example.invalid":
			return notification.Receipt{Accepted: true}, nil
		case "rejected@example.invalid":
			return notification.Receipt{Rejected: true}, nil
		case "retry@example.invalid":
			if counts[m.To] > 1 {
				return notification.Receipt{Accepted: true}, nil
			}
			return notification.Receipt{Temporary: true}, nil
		default:
			return notification.Receipt{}, nil
		}
	})}
	for range 5 {
		if err := n.Pump(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
	deliveries, err := r.Deliveries(e.ctx, "", 10)
	if err != nil || len(deliveries) != 4 {
		t.Fatal(deliveries, err)
	}
	expected := map[string]string{"accepted@example.invalid": "accepted", "rejected@example.invalid": "rejected", "unknown@example.invalid": "unknown", "retry@example.invalid": "retry"}
	for _, d := range deliveries {
		if d.Status != expected[d.Recipient] || d.Attempts != 1 {
			t.Fatal(d)
		}
	}
	if _, err = c.Exec(e.ctx, "UPDATE control.notification_deliveries SET next_attempt_at=clock_timestamp()-interval '1 second' WHERE status='retry'"); err != nil {
		t.Fatal(err)
	}
	if err = n.Pump(e.ctx); err != nil {
		t.Fatal(err)
	}
	if counts["retry@example.invalid"] != 2 || counts["unknown@example.invalid"] != 1 {
		t.Fatal("unknown resent or retry lost", counts)
	}
}
func TestOI06_04(t *testing.T) {
	oiEvidence(t)
	t.Run("close_claim_order", TestT33_06To11PostgresClaimCloseRecoveryAndBudgets)
	t.Run("rebuild_and_reenable", TestT33_18And19NotifierRebuildDynamicRecipientsAndUnavailableLogs)
}
func TestOI06_05(t *testing.T) {
	oiEvidence(t)
	t.Run("log_failure_idempotency_saved_test", TestT33_18And19NotifierRebuildDynamicRecipientsAndUnavailableLogs)
}
