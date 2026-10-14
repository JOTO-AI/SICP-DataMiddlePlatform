package admin

import (
	"context"
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type eventFilterDB struct {
	treeViewDB
	task operations.Event
	args []any
}

func (d *eventFilterDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	d.calls++
	return d, nil
}
func (d *eventFilterDB) Query(_ context.Context, _ string, a ...any) (storage.Rows, error) {
	d.args = a
	return &eventFilterRows{task: d.task}, nil
}

type eventFilterRows struct {
	task operations.Event
	done bool
}

func (r *eventFilterRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}
func (*eventFilterRows) Close()     {}
func (*eventFilterRows) Err() error { return nil }
func (r *eventFilterRows) Scan(d ...any) error {
	*d[0].(*string) = r.task.TaskID
	*d[1].(*string) = r.task.Mode
	*d[2].(*string) = r.task.Status
	*d[4].(*time.Time) = r.task.AcceptedAt
	return nil
}
func TestFrontendUpdateFilterHTTPAuthAndCursorBinding(t *testing.T) {
	raw, e := os.ReadFile("../../development_docs/verification/operations_http_examples.json")
	if e != nil {
		t.Fatal(e)
	}
	var document struct {
		Examples []struct {
			Name     string
			Response struct{ Data json.RawMessage }
		}
	}
	if e = json.Unmarshal(raw, &document); e != nil {
		t.Fatal(e)
	}
	var task operations.Event
	for _, v := range document.Examples {
		if v.Name == "update_detail" {
			if e = json.Unmarshal(v.Response.Data, &task); e != nil {
				t.Fatal(e)
			}
		}
	}
	if task.TaskID == "" {
		t.Fatal("missing backend fixture")
	}
	auth, e := NewSessions("admin", "synthetic-admin-password", "query-key", false)
	if e != nil {
		t.Fatal(e)
	}
	db := &eventFilterDB{task: task}
	tokens := operations.NewTokens(strings.Repeat("k", 32))
	views := OperationsViews{Repository: operations.Repository{Database: db}, Tokens: tokens}
	mux := http.NewServeMux()
	if e = views.Mount(mux, auth, StatusHandler{Store: &acquisitionStatusStore{}}); e != nil {
		t.Fatal(e)
	}
	cookie, _ := loginSession(t, auth)
	get := func(query string, login bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/admin/v1/update-events?"+query, nil)
		if login {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := get("page_size=50", false); w.Code != 401 || db.calls != 0 {
		t.Fatal(w.Code)
	}
	q := url.Values{"from": {task.AcceptedAt.Add(-time.Hour).Format(time.RFC3339)}, "to": {task.AcceptedAt.Add(time.Hour).Format(time.RFC3339)}, "mode": {task.Mode}, "status": {task.Status}, "task_id": {task.TaskID}}
	w := get(q.Encode(), true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body struct{ Data operations.EventPage }
	if e = json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatal(e)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].TaskID != task.TaskID || body.Data.Filter == nil || body.Data.Filter.TaskID != task.TaskID || len(db.args) != 8 || db.args[7] != task.TaskID {
		t.Fatal(body, db.args)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	cursor, e := tokens.Encode(operations.Token{Scope: "update-events", Subject: "admin", At: now, Expires: now.Add(time.Hour), Size: 50, Events: body.Data.Filter})
	if e != nil {
		t.Fatal(e)
	}
	if w = get(url.Values{"cursor": {cursor}}.Encode(), true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := db.calls
	for _, q := range []string{"page=10", "mode=incremental&mode=force_full", "cursor=" + url.QueryEscape(cursor) + "&status=failed"} {
		if w = get(q, true); w.Code != 400 || db.calls != before {
			t.Fatal(q, w.Code, w.Body.String())
		}
	}
}
