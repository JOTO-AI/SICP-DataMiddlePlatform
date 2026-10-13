package operations

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type fakeRow func(...any) error

func (f fakeRow) Scan(v ...any) error { return f(v...) }

type fakeDB struct{ tx *fakeTx }

func (f fakeDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	return f.tx, nil
}

type fakeTx struct {
	row               func(string, []any) storage.Row
	exec              func(string, []any) (int64, error)
	committed, rolled bool
}

func (f *fakeTx) QueryRow(_ context.Context, q string, a ...any) storage.Row { return f.row(q, a) }
func (f *fakeTx) Exec(_ context.Context, q string, a ...any) (int64, error) {
	if f.exec != nil {
		return f.exec(q, a)
	}
	return 1, nil
}
func (f *fakeTx) Commit(context.Context) error   { f.committed = true; return nil }
func (f *fakeTx) Rollback(context.Context) error { f.rolled = true; return nil }
func fixtureRepo(tx *fakeTx) Repository          { return Repository{Database: fakeDB{tx}} }
func TestT31_04FourteenUnknownSourcesRemainNull(t *testing.T) {
	tx := &fakeTx{row: func(q string, args []any) storage.Row {
		return fakeRow(func(d ...any) error {
			if strings.Contains(q, "roles::text") {
				*d[0].(**string) = nil
				return nil
			}
			if !strings.Contains(q, "WHERE source=$1") || len(args) != 1 {
				return errors.New("unbound source")
			}
			*d[0].(**string) = nil
			return nil
		})
	}}
	items, e := fixtureRepo(tx).Sources(t.Context(), func(s string) string { return "digest-" + s })
	if e != nil || len(items) != 14 {
		t.Fatal(e, len(items))
	}
	for _, s := range items {
		if s.ID == "" || s.PrimaryKey == "" || s.ConfirmedUntil != nil || s.CheckedAt != nil || s.TaskID != nil || s.Window != nil {
			t.Fatal("invented source fact")
		}
	}
}
func TestT31_01And03UnknownOperationNeverBecomesSuccess(t *testing.T) {
	tx := &fakeTx{row: func(q string, args []any) storage.Row {
		return fakeRow(func(d ...any) error { *d[0].(*bool) = true; return nil })
	}}
	nodes, e := fixtureRepo(tx).Operations(t.Context(), "task-1", "", "", 10)
	if e != nil || len(nodes) != 6 {
		t.Fatal(e)
	}
	for _, node := range nodes {
		if node.State != "unknown" || node.Count != nil || node.StartedAt != nil || node.FinishedAt != nil {
			t.Fatal("fabricated operation completion")
		}
	}
	nodes, e = fixtureRepo(tx).Operations(t.Context(), "task-1", "", "builds", 10)
	if e != nil || len(nodes) != 4 || nodes[0].ID != "cleanup" {
		t.Fatal("operation keyset ordering")
	}
}
func TestT31_10ScopedCursorAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	tokens := NewTokens(strings.Repeat("a", 32))
	v := Token{Scope: "events", Subject: "alice", Position: "task-1", Size: 50, At: now, Expires: now.Add(time.Hour)}
	raw, e := tokens.Encode(v)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tokens.Decode(raw, "events", "bob", now); e != ErrForbidden {
		t.Fatal(e)
	}
	if _, e = tokens.Decode(raw, "operations", "alice", now); e != ErrArgument {
		t.Fatal(e)
	}
	if _, e = tokens.Decode(raw, "events", "alice", now.Add(time.Hour)); e != ErrConflict {
		t.Fatal(e)
	}
	if _, e = tokens.Decode(raw+"x", "events", "alice", now); e != ErrArgument {
		t.Fatal(e)
	}
}
func TestT32_01To04VersionedCatalogAndSnapshotCapabilities(t *testing.T) {
	items := Catalog()
	if len(items) != 23 {
		t.Fatal(len(items))
	}
	seen := map[string]bool{}
	sources, business, snapshots := 0, 0, 0
	for _, api := range items {
		if seen[api.ID] || api.Version != CatalogVersion || api.Path == "" || api.Request.Properties == nil || api.Response.Properties == nil || api.Readiness != "unknown" {
			t.Fatal("invalid catalog")
		}
		seen[api.ID] = true
		if api.Kind == "source" {
			sources++
			if api.Method != "GET" || api.Snapshot {
				t.Fatal("source method")
			}
		} else {
			business++
			if api.Method != "POST" {
				t.Fatal("business method")
			}
		}
		if api.Snapshot {
			snapshots++
		}
	}
	if sources != 14 || business != 9 || snapshots != 7 {
		t.Fatal(sources, business, snapshots)
	}
	api, _ := LookupAPI("teaching-classes.students.query")
	if !api.Request.Properties["planned_course_code"].Required || api.Request.Properties["page_size"].Default != 100 {
		t.Fatal("roster contract")
	}
}
func TestT32_11SourceRequestWhitelistAndBudget(t *testing.T) {
	r := TestRequest{APIID: "dwd_bzkscjzlb", Version: CatalogVersion}
	if e := r.Normalize(); e != nil || r.Page != 1 || r.PageSize != 10 {
		t.Fatal(e)
	}
	for _, v := range []TestRequest{{APIID: "students.query", Version: CatalogVersion}, {APIID: "http://evil.invalid", Version: CatalogVersion}, {APIID: r.APIID, Version: "old"}, {APIID: r.APIID, Version: CatalogVersion, Page: 1, PageSize: 101}} {
		if v.Normalize() != ErrArgument {
			t.Fatal("invalid source request accepted")
		}
	}
}
func TestT32_13To15ResultOwnershipExpiryEvictionAndCopy(t *testing.T) {
	r := NewResults()
	now := time.Now()
	raw := []byte(`{"synthetic":"one"}`)
	if !r.Put("run", "alice", raw, now) {
		t.Fatal("put")
	}
	raw[2] = 'x'
	value, ok := r.Get("run", "alice", now)
	if !ok || string(value) != `{"synthetic":"one"}` {
		t.Fatal("mutable retained input")
	}
	value[2] = 'x'
	if _, ok = r.Get("run", "bob", now); ok {
		t.Fatal("cross subject disclosure")
	}
	if _, ok = r.Get("run", "alice", now.Add(10*time.Minute)); ok {
		t.Fatal("expired payload returned")
	}
	large := []byte(`"` + strings.Repeat("s", (2<<20)-2) + `"`)
	for i := 0; i < 11; i++ {
		if !r.Put(string(rune('a'+i)), "alice", large, now.Add(time.Duration(i)*time.Second)) {
			t.Fatal("bounded store")
		}
	}
	if r.bytes > 20<<20 || len(r.items) > 10 {
		t.Fatal("memory unbounded")
	}
	if _, ok = r.Get("a", "alice", now); ok {
		t.Fatal("oldest result not evicted")
	}
}
func TestT33_01To05SettingsValidationAndEmptyAtomicDisable(t *testing.T) {
	s := NotificationSettings{Revision: 2, Enabled: true, Recipients: []string{" A@EXAMPLE.INVALID ", "A@example.invalid", "b@example.invalid"}, Rules: map[string]bool{"task_failed": true}}
	if e := s.Normalize(true); e != nil || !reflect.DeepEqual(s.Recipients, []string{"A@example.invalid", "b@example.invalid"}) {
		t.Fatal(s, e)
	}
	s.Recipients = nil
	if e := s.Normalize(false); e != nil || s.Enabled || s.Recipients == nil {
		t.Fatal("clearing did not disable")
	}
	for _, email := range []string{"a@example.invalid\r\nBcc:x@y.invalid", "Name <a@example.invalid>", "missing-at", ""} {
		if _, e := NormalizeAddress(email); e != ErrArgument {
			t.Fatal("invalid address accepted")
		}
	}
	s = NotificationSettings{Revision: 1, Enabled: true, Recipients: []string{"a@example.invalid"}}
	if s.Normalize(false) != ErrUnavailable {
		t.Fatal("enabled missing channel")
	}
}
func storedSettings() storage.Row {
	return fakeRow(func(d ...any) error {
		*d[0].(*int64) = 2
		*d[1].(*bool) = true
		*d[2].(*string) = `["a@example.invalid"]`
		*d[3].(*string) = `{"task_failed":true}`
		*d[4].(*time.Time) = time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
		return nil
	})
}
func TestT33_02RevisionConflictDoesNotMutate(t *testing.T) {
	writes := 0
	tx := &fakeTx{row: func(string, []any) storage.Row { return storedSettings() }, exec: func(string, []any) (int64, error) { writes++; return 1, nil }}
	_, e := fixtureRepo(tx).SaveNotifications(t.Context(), NotificationSettings{Revision: 1, Recipients: []string{}, Rules: map[string]bool{}}, true, "actor", time.Now())
	if e != ErrConflict || writes != 0 || tx.committed || !tx.rolled {
		t.Fatal("conflicting edit wrote state")
	}
}
func TestT33_06To11PerRecipientOutcomesAndFiniteRetry(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	owner := "process-1"
	for _, tc := range []struct {
		name    string
		receipt notification.Receipt
		err     error
		attempt int
		want    string
	}{{"accepted", notification.Receipt{Accepted: true}, nil, 1, "accepted"}, {"unknown", notification.Receipt{}, errors.New("reply lost"), 1, "unknown"}, {"temporary", notification.Receipt{Temporary: true}, errors.New("dial"), 1, "retry"}, {"third", notification.Receipt{Temporary: true}, errors.New("dial"), 3, "exhausted"}, {"rejected", notification.Receipt{Rejected: true}, errors.New("550"), 1, "rejected"}} {
		t.Run(tc.name, func(t *testing.T) {
			status := ""
			var next *time.Time
			tx := &fakeTx{row: func(string, []any) storage.Row { return storedSettings() }, exec: func(q string, a []any) (int64, error) {
				if strings.Contains(q, "UPDATE control.notification_deliveries") {
					status = a[3].(string)
					next = a[4].(*time.Time)
				}
				return 1, nil
			}}
			d := Delivery{ID: "delivery-1", Rule: "task_failed", Recipient: "a@example.invalid", Attempts: tc.attempt, Owner: &owner, Deadline: now.Add(24 * time.Hour)}
			if e := fixtureRepo(tx).FinishDelivery(t.Context(), d, tc.receipt, tc.err, now); e != nil || status != tc.want {
				t.Fatal(status, e)
			}
			if tc.want == "retry" && (next == nil || !next.Equal(now.Add(time.Minute))) {
				t.Fatal("wrong retry delay")
			}
			if tc.want == "unknown" && next != nil {
				t.Fatal("unknown was automatically retried")
			}
		})
	}
}
func TestT34_01RetentionBoundsAndUnknownTimes(t *testing.T) {
	for _, s := range []Retention{{0, 1, 1}, {1, 0, 30}, {1, 30, 3651}} {
		if s.Valid() {
			t.Fatal("invalid retention")
		}
	}
	if !(Retention{1, 30, 30}).Valid() {
		t.Fatal("default invalid")
	}
}
func TestT32_17CatalogJSONHasNoCredentials(t *testing.T) {
	raw, e := json.Marshal(Catalog())
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"access_token", "smtp_password", "source_app_secret", "DATABASE_LOG_URL"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("credential field in catalog")
		}
	}
}
