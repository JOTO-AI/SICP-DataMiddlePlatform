package logquery

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type readerFunc func(context.Context, Source, ReadRequest) (Batch, error)

func (f readerFunc) Read(ctx context.Context, s Source, r ReadRequest) (Batch, error) {
	return f(ctx, s, r)
}
func readFixture(t *testing.T) (*observability.Sanitizer, Record, *Signer, time.Time) {
	t.Helper()
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	s, err := observability.NewSanitizer(MaxEventBytes, 2048, []byte(strings.Repeat("a", 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.Sanitize(events.Event{ID: "event-1", EntityID: "task-1", Kind: events.Task, Phase: events.Started, Identity: events.Identity{TaskID: "task-1"}, OccurredAt: at}, observability.Details{Message: "private personnel value"})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSigner([]byte(strings.Repeat("b", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return s, Record{Sequence: 9, ReceivedAt: at.Add(time.Second), Event: e}, signer, at.Add(time.Minute)
}
func TestT29_05StableHistoryAndOriginalLiveBoundary(t *testing.T) {
	s, rec, signer, now := readFixture(t)
	calls := 0
	reader := readerFunc(func(_ context.Context, _ Source, r ReadRequest) (Batch, error) {
		calls++
		b := Batch{Snapshot: Snapshot{"epoch-1", 10, 0}}
		if calls == 1 {
			b.Records = []Record{rec, {Sequence: 8, ReceivedAt: rec.ReceivedAt, Event: rec.Event}}
		} else {
			if r.Cursor == nil || r.Cursor.HighWater != 10 || r.Cursor.Position != 9 {
				t.Fatal("snapshot or position changed")
			}
			b.Snapshot.HighWater = 15
		}
		return b, nil
	})
	svc, _ := NewService(reader, signer, s)
	svc.Now = func() time.Time { return now }
	first, err := svc.List(t.Context(), Platform, "operator", url.Values{"mode": {"live"}, "page_size": {"1"}})
	if err != nil || len(first.Items) != 1 || first.NextCursor == nil || first.StreamCursor == nil {
		t.Fatal(first, err)
	}
	now = now.Add(time.Hour)
	second, err := svc.List(t.Context(), Platform, "operator", url.Values{"cursor": {*first.NextCursor}})
	if err != nil || len(second.Items) != 0 || second.NextCursor != nil || second.HighWater != 10 || *first.StreamCursor != *second.StreamCursor || !second.Filter.From.Equal(first.Filter.From) {
		t.Fatal(second, err)
	}
}
func TestT29_06BoundParametersAndTimeWindow(t *testing.T) {
	f := Filter{Mode: HistoryMode, From: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), PageSize: 50, TaskID: strings.Repeat("c", 64), Kind: events.Task}
	sql, args := readSQL(ReadRequest{Filter: f}, Snapshot{"epoch", 99, 0})
	if strings.Contains(sql, f.TaskID) || !strings.Contains(sql, "occurred_at >= $3") || !strings.Contains(sql, "occurred_at < $4") || len(args) != 11 || args[8] != f.TaskID || args[0] != int64(99) {
		t.Fatal("unbound/intersection query")
	}
	f.Mode = LiveMode
	f.To = time.Time{}
	_, args = readSQL(ReadRequest{Filter: f}, Snapshot{"epoch", 99, 0})
	if args[3] != nil {
		t.Fatal("live upper bound")
	}
}
func TestT29_07ExpiredFloorAndNoPartialFailure(t *testing.T) {
	s, _, signer, now := readFixture(t)
	svc, _ := NewService(readerFunc(func(context.Context, Source, ReadRequest) (Batch, error) { return Batch{}, ErrExpired }), signer, s)
	svc.Now = func() time.Time { return now }
	page, err := svc.List(t.Context(), Platform, "actor", nil)
	if err != ErrExpired || len(page.Items) != 0 {
		t.Fatal("failure became empty success")
	}
}
func TestT29_08DetailValidMissingAndNoRawContent(t *testing.T) {
	s, rec, signer, _ := readFixture(t)
	missing := false
	svc, _ := NewService(readerFunc(func(_ context.Context, source Source, r ReadRequest) (Batch, error) {
		if source != Platform || r.EventID != rec.Event.ID() {
			t.Fatal("identity changed")
		}
		b := Batch{Snapshot: Snapshot{"epoch-1", 9, 0}}
		if !missing {
			b.Records = []Record{rec}
		}
		return b, nil
	}), signer, s)
	d, err := svc.Detail(t.Context(), Platform, rec.Event.ID())
	if err != nil || d.Sequence != 9 || !d.Redaction.Redacted || d.Redaction.OriginalBytes != 23 || d.TaskID == nil || *d.TaskID != s.Correlation("task-1") {
		t.Fatal(d, err)
	}
	missing = true
	if _, err = svc.Detail(t.Context(), Platform, rec.Event.ID()); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err = svc.Detail(t.Context(), Platform, "raw-task"); err != ErrArgument {
		t.Fatal(err)
	}
}

type queryDB struct{ tx *queryTx }

func (d queryDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	return d.tx, nil
}

type queryTx struct {
	rec                                 Record
	mode                                string
	closed, rolled, committed, snapshot bool
	query                               string
}
type queryRow func(...any) error

func (r queryRow) Scan(d ...any) error { return r(d...) }
func (t *queryTx) Exec(_ context.Context, q string, _ ...any) (int64, error) {
	if q != "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY" {
		return 0, errors.New("unexpected")
	}
	t.snapshot = true
	return 0, nil
}
func (t *queryTx) QueryRow(_ context.Context, q string, _ ...any) storage.Row {
	return queryRow(func(d ...any) error {
		if !t.snapshot || !strings.Contains(q, "NOT has_function_privilege") {
			return errors.New("unsafe snapshot")
		}
		*d[0].(*string) = "epoch-1"
		*d[1].(*int64) = 9
		*d[2].(*int64) = 0
		return nil
	})
}
func (t *queryTx) Query(_ context.Context, q string, _ ...any) (storage.Rows, error) {
	t.query = q
	if t.mode == "query" {
		return nil, errors.New("private SQL failure")
	}
	return &queryRows{tx: t}, nil
}
func (t *queryTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *queryTx) Rollback(context.Context) error { t.rolled = true; return nil }

type queryRows struct {
	tx *queryTx
	n  int
}

func (r *queryRows) Next() bool { r.n++; return r.n == 1 }
func (r *queryRows) Close()     { r.tx.closed = true }
func (r *queryRows) Err() error {
	if r.tx.mode == "rows" {
		return errors.New("rows")
	}
	return nil
}
func (r *queryRows) Scan(d ...any) error {
	if r.tx.mode == "scan" {
		return errors.New("scan")
	}
	rec := r.tx.rec
	raw := rec.Event.Bytes()
	*d[0].(*string) = rec.Event.ID()
	*d[1].(*int64) = rec.Sequence
	*d[2].(*time.Time) = rec.ReceivedAt
	*d[3].(*int) = len(raw)
	*d[4].(*string) = string(raw)
	if r.tx.mode == "large" {
		*d[3].(*int) = MaxEventBytes + 1
		*d[4].(*string) = ""
	}
	if r.tx.mode == "corrupt" {
		*d[4].(*string) = `{"event":{"raw":"private"}}`
	}
	return nil
}
func TestT29_09RepositorySnapshotFailureAndRecovery(t *testing.T) {
	s, rec, _, now := readFixture(t)
	v := map[string]string{"LOG_STORAGE_MODE": "single_legacy", "DATABASE_M0_URL": "postgres://x@m:5432/b0?sslmode=disable", "DATABASE_M1_URL": "postgres://x@m:5432/b1?sslmode=disable", "DATABASE_S0_URL": "postgres://x@s:5432/b0?sslmode=disable", "DATABASE_S1_URL": "postgres://x@s:5432/b1?sslmode=disable", "DATABASE_CONTROL_URL": "postgres://x@m:5432/control?sslmode=disable", "DATABASE_LOG_URL": "postgres://writer@l:5432/logs?sslmode=disable", "DATABASE_LOG_READ_URL": "postgres://reader@l:5432/logs?sslmode=disable"}
	registry, err := storage.LoadRegistry(func(k string) (string, bool) { s, ok := v[k]; return s, ok })
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"", "query", "scan", "rows", "large", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			tx := &queryTx{rec: rec, mode: mode}
			repo := Repository{Registry: registry, Database: queryDB{tx}, Sanitizer: s}
			b, err := repo.Read(t.Context(), Platform, ReadRequest{Filter: Filter{Mode: HistoryMode, From: now.Add(-time.Hour), To: now, PageSize: 50}})
			if mode == "" {
				if err != nil || len(b.Records) != 1 || !tx.committed {
					t.Fatal(err)
				}
			} else if err == nil || len(b.Records) != 0 || tx.committed {
				t.Fatal("partial success")
			}
			if !tx.rolled || mode != "query" && !tx.closed {
				t.Fatal("resource leak")
			}
			if !strings.Contains(tx.query, "octet_length") {
				t.Fatal("unbounded payload fetch")
			}
		})
	}
}
func TestT29_10ConcurrentReadBudgetReleased(t *testing.T) {
	s, _, signer, _ := readFixture(t)
	svc, _ := NewService(readerFunc(func(context.Context, Source, ReadRequest) (Batch, error) { return Batch{}, ErrUnavailable }), signer, s)
	for range 8 {
		svc.reads <- struct{}{}
	}
	if _, e := svc.Detail(t.Context(), Platform, strings.Repeat("a", 64)); e != ErrLimit {
		t.Fatal(e)
	}
	<-svc.reads
	if _, e := svc.Detail(t.Context(), Platform, strings.Repeat("a", 64)); e != ErrUnavailable {
		t.Fatal(e)
	}
	if len(svc.reads) != 7 {
		t.Fatal("budget leaked")
	}
}
