package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type treeViewDB struct {
	calls int
	fail  bool
}

func (d *treeViewDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	d.calls++
	if d.fail {
		return nil, errors.New("private control failure")
	}
	return d, nil
}
func (*treeViewDB) Exec(context.Context, string, ...any) (int64, error) { return 1, nil }

type treeViewRow struct{ exists bool }

func (r treeViewRow) Scan(v ...any) error { *v[0].(*bool) = r.exists; return nil }
func (*treeViewDB) QueryRow(_ context.Context, _ string, a ...any) storage.Row {
	return treeViewRow{a[0] == "task-a"}
}
func (*treeViewDB) Commit(context.Context) error   { return nil }
func (*treeViewDB) Rollback(context.Context) error { return nil }
func (*treeViewDB) Query(_ context.Context, _ string, a ...any) (storage.Rows, error) {
	ids := []string{}
	for _, id := range []string{"maintenance/version-a", "maintenance/version-b"} {
		if id > a[1].(string) {
			ids = append(ids, id)
		}
	}
	return &treeViewRows{ids: ids}, nil
}

type treeViewRows struct {
	ids []string
	pos int
}

func (r *treeViewRows) Next() bool {
	if r.pos == len(r.ids) {
		return false
	}
	r.pos++
	return true
}
func (*treeViewRows) Close()     {}
func (*treeViewRows) Err() error { return nil }
func (r *treeViewRows) Scan(v ...any) error {
	*v[0].(*string), *v[1].(*string), *v[7].(*string) = r.ids[r.pos-1], "succeeded", "cleanup"
	return nil
}

func TestT31_01And03EventDetailAndTreeRoutesKeepIdentityAndCursorScope(t *testing.T) {
	auth, err := NewSessions("admin", "synthetic-admin-password", "query-key", false)
	if err != nil {
		t.Fatal(err)
	}
	db := &treeViewDB{}
	views := OperationsViews{Repository: operations.Repository{Database: db}, Tokens: operations.NewTokens(strings.Repeat("k", 32))}
	mux := http.NewServeMux()
	if err = views.Mount(mux, auth, StatusHandler{Store: &acquisitionStatusStore{}}); err != nil {
		t.Fatal(err)
	}
	cookie, _ := loginSession(t, auth)
	get := func(path string, login bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if login {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	base := "/admin/v1/update-events/task-a/operations"
	if w := get(base, false); w.Code != 401 || db.calls != 0 {
		t.Fatal("unauthenticated control access", w.Code)
	}
	if w := get("/admin/v1/update-events/task-a", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"task_id":"task-a"`) {
		t.Fatal("event detail lost task identity", w.Code, w.Body.String())
	}
	first := get(base+"?parent_operation_id=cleanup&page_size=1", true)
	var page struct {
		Data struct {
			Items  []operations.Operation `json:"items"`
			Cursor *string                `json:"next_cursor"`
		} `json:"data"`
	}
	if json.Unmarshal(first.Body.Bytes(), &page) != nil || first.Code != 200 || len(page.Data.Items) != 1 || page.Data.Cursor == nil {
		t.Fatal("missing first page", first.Body.String())
	}
	if page.Data.Items[0].ID != "maintenance/version-a" || page.Data.Items[0].Parent == nil || *page.Data.Items[0].Parent != "cleanup" {
		t.Fatal("wrong parent", page)
	}
	cursor := url.QueryEscape(*page.Data.Cursor)
	second := get(base+"?parent_operation_id=cleanup&cursor="+cursor, true)
	page.Data.Items = nil
	page.Data.Cursor = nil
	if json.Unmarshal(second.Body.Bytes(), &page) != nil || second.Code != 200 || len(page.Data.Items) != 1 || page.Data.Items[0].ID != "maintenance/version-b" || page.Data.Cursor != nil {
		t.Fatal("cleanup cursor rejected or repeated", second.Body.String())
	}
	for _, path := range []string{
		base + "?parent_operation_id=builds&cursor=" + cursor,
		"/admin/v1/update-events/other/operations?parent_operation_id=cleanup&cursor=" + cursor,
		base + "?parent_operation_id=cleanup&page_size=2&cursor=" + cursor,
		base + "?parent_operation_id=cleanup&parent_operation_id=trace",
	} {
		before := db.calls
		if w := get(path, true); w.Code != 400 || db.calls != before {
			t.Fatal("cursor scope or malformed query accepted", w.Code, w.Body.String())
		}
	}
	if w := get("/admin/v1/update-events/missing/operations", true); w.Code != 404 {
		t.Fatal("missing event became empty success", w.Code)
	}
	// The unavailable log reader must fail its subtree without hiding control.
	if w := get(base+"?parent_operation_id=trace:drain", true); w.Code != 503 {
		t.Fatal("missing logs reported empty success", w.Code)
	}
	if w := get(base, true); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"unknown"`) {
		t.Fatal("log failure hid control tree", w.Code)
	}
	db.fail = true
	if w := get(base, true); w.Code != 503 || strings.Contains(w.Body.String(), "private control") || strings.Contains(w.Body.String(), `"items"`) {
		t.Fatal("control failure leaked partial tree", w.Code, w.Body.String())
	}
}
