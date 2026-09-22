package control_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type outboxDB struct {
	mu                    sync.Mutex
	rows                  map[string][]any
	knownTask             string
	progress              time.Time
	calls                 []dbCall
	connections           []string
	failCommit, lostReply bool
}
type outboxTx struct {
	db     *outboxDB
	rows   map[string][]any
	closed bool
}

func (d *outboxDB) Begin(_ context.Context, c storage.Connection) (storage.Transaction, error) {
	if !strings.Contains(c.DSN(), "/control?") {
		return nil, errors.New("wrong fixed connection")
	}
	d.mu.Lock()
	d.connections = append(d.connections, c.DSN())
	rows := map[string][]any{}
	for k, v := range d.rows {
		rows[k] = append([]any(nil), v...)
	}
	return &outboxTx{db: d, rows: rows}, nil
}
func (x *outboxTx) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	x.db.calls = append(x.db.calls, dbCall{sql, args})
	if !strings.HasPrefix(sql, "INSERT INTO control.notification_outbox ") {
		return 0, errors.New("unexpected control mutation")
	}
	if len(args) != 5 || args[2] != x.db.knownTask {
		return 0, errors.New("task FK not satisfied")
	}
	key := args[1].(string)
	if _, ok := x.rows[key]; ok {
		return 0, nil
	}
	x.rows[key] = []any{args[0], args[1], args[2], args[3], "pending", 0, args[4].(time.Time).Truncate(time.Microsecond), nil, nil}
	return 1, nil
}
func (x *outboxTx) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	x.db.calls = append(x.db.calls, dbCall{sql, args})
	if strings.Contains(sql, `"failure_key"=$1`) {
		return result{raw: x.rows[args[0].(string)]}
	}
	for _, row := range x.rows {
		if row[0] == args[0] {
			return result{raw: row}
		}
	}
	return result{}
}
func (x *outboxTx) Commit(context.Context) error {
	if x.db.failCommit {
		return errors.New("control unavailable secret")
	}
	x.db.rows = x.rows
	x.closed = true
	x.db.mu.Unlock()
	if x.db.lostReply {
		x.db.lostReply = false
		return errors.New("commit response lost")
	}
	return nil
}
func (x *outboxTx) Rollback(context.Context) error {
	if !x.closed {
		x.closed = true
		x.db.mu.Unlock()
	}
	return nil
}

type notificationRows struct {
	rows  [][]any
	index int
}

func (r *notificationRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}
func (r *notificationRows) Scan(dest ...any) error {
	return (result{raw: r.rows[r.index-1]}).Scan(dest...)
}
func (r *notificationRows) Err() error { return nil }
func (r *notificationRows) Close()     {}
func (x *outboxTx) Query(_ context.Context, sql string, args ...any) (storage.Rows, error) {
	if !strings.Contains(sql, "FROM control.notification_outbox WHERE status='pending'") {
		return nil, errors.New("unexpected recovery query")
	}
	var keys []string
	for key, row := range x.rows {
		if row[4] == "pending" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var rows [][]any
	for _, key := range keys {
		if len(rows) == args[0].(int) {
			break
		}
		rows = append(rows, x.rows[key])
	}
	return &notificationRows{rows: rows}, nil
}
func notificationFixture(t *testing.T) (control.Repository, *outboxDB, *observability.Sanitizer, *notification.Consumer) {
	t.Helper()
	r, _ := repository(t)
	d := &outboxDB{rows: map[string][]any{}, knownTask: "task-new", progress: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)}
	r.Database = d
	s, err := observability.NewSanitizer(4096, 256, []byte(strings.Repeat("k", 32)), []string{"TOPSECRET_TOKEN", "fictional-secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := notification.NewConsumer(r, s, "test", "NEW_COLUMN", "EXTRA", "NORMAL_FIELD")
	if err != nil {
		t.Fatal(err)
	}
	return r, d, s, consumer
}

type notificationTransport func(*http.Request) (*http.Response, error)

func (f notificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func sourceFinal(t *testing.T, mode ingest.Mode, records []string, sink events.Sink) (*ingest.FailureEvent, error) {
	t.Helper()
	attempt := 0
	client, err := ingest.NewClient("https://source.invalid", "fictional-secret-token", notificationTransport(func(*http.Request) (*http.Response, error) {
		record := records[min(attempt, len(records)-1)]
		attempt++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":1,"list":[` + record + `]}}`))}, nil
	}), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if sink == nil {
		sink = events.SinkFunc(func(context.Context, events.Event) error { return nil })
	}
	recorder, err := events.New(testkit.FakeClock{Time: time.Date(2026, 9, 22, 0, 0, 0, 123456789, time.UTC)}, testkit.FixedRandom(10), sink, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := events.WithRecorder(context.Background(), recorder, events.Identity{TaskID: "task-new"})
	r, err := client.FetchPage(ctx, ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: mode, Page: 1, PageSize: 2, StartTime: "2026-09-21 00:00:00", EndTime: "2026-09-22 00:00:00"}, ingest.RetryPolicy{MaxAttempts: 2, Wait: func(context.Context, int) error { return nil }})
	return r.FinalFailure, err
}
func TestT05_12FinalUnknownFieldsPersistOneNoticeAndDoNotAdvanceProgress(t *testing.T) {
	for _, mode := range []ingest.Mode{ingest.Full, ingest.Incremental} {
		for _, value := range []string{`null`, `"Person-Fictional"`} {
			r, db, _, consumer := notificationFixture(t)
			progress := db.progress
			final, err := sourceFinal(t, mode, []string{`{"XQH":"01","NEW_COLUMN":` + value + `}`}, nil)
			if err == nil || final == nil {
				t.Fatal("unknown field did not fail")
			}
			row, created, err := consumer.Consume(context.Background(), final)
			if err != nil || !created || row.TaskID != "task-new" || row.Status != "pending" || len(db.rows) != 1 || !db.progress.Equal(progress) {
				t.Fatal("durable outbox or progress boundary failed")
			}
			var payload notification.Payload
			if json.Unmarshal([]byte(row.Payload), &payload) != nil || payload.Notice.Attempts != 2 || payload.Notice.Mode != mode || payload.Notice.Fields[0].Name != "NEW_COLUMN" || payload.Notice.TaskID == row.TaskID {
				t.Fatal("notice facts or redacted identity incorrect")
			}
			pending, err := r.PendingNotifications(context.Background(), 10)
			if err != nil || len(pending) != 1 {
				t.Fatal("pending recovery read failed")
			}
			if strings.Contains(string(row.Payload), "Person-Fictional") {
				t.Fatal("field value reached outbox")
			}
		}
	}
}
func TestT05_13RecoveredAndKnownColumnsDoNotCreateFinalMail(t *testing.T) {
	for _, records := range [][]string{{`{"XQH":"01","EXTRA":null}`, `{"XQH":"01"}`}, {`{"XQH":"01","XQMC":"Person-Fictional"}`}} {
		_, db, _, consumer := notificationFixture(t)
		final, err := sourceFinal(t, ingest.Full, records, nil)
		if err != nil || final != nil {
			t.Fatal("recovered or known column classified final failure")
		}
		if _, created, err := consumer.Consume(context.Background(), final); err != nil || created || len(db.rows) != 0 {
			t.Fatal("nonfinal failure created notification")
		}
	}
	_, _, _, consumer := notificationFixture(t)
	if _, _, err := consumer.Consume(context.Background(), &ingest.FailureEvent{}); err != notification.ErrNotice {
		t.Fatal("handmade nonfinal event accepted")
	}
}
func TestT05_14ReplayIsIdempotentAndConflictingFactIsRejected(t *testing.T) {
	_, db, _, consumer := notificationFixture(t)
	final, _ := sourceFinal(t, ingest.Full, []string{`{"XQH":"01","EXTRA":null}`}, nil)
	db.lostReply = true
	if _, _, err := consumer.Consume(context.Background(), final); err != notification.ErrOutbox || len(db.rows) != 1 {
		t.Fatal("lost control ack swallowed")
	}
	if _, created, err := consumer.Consume(context.Background(), final); err != nil || created || len(db.rows) != 1 {
		t.Fatal("replay duplicated final failure")
	}
	changed := *final
	changed.UnknownFields = []string{"DIFFERENT"}
	if _, _, err := consumer.Consume(context.Background(), &changed); err != notification.ErrConflict || len(db.rows) != 1 {
		t.Fatal("conflicting failure silently overwritten")
	}
}
func TestT05_16RebuildRecoversControlOutboxWhileLogPGUnavailable(t *testing.T) {
	r, db, s, consumer := notificationFixture(t)
	files, err := observability.OpenDirectory(t.TempDir(), testkit.FixedRandom(11), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	q, _ := observability.NewQuota(files, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 100, WarningPercent: 80, Policy: observability.RejectNew})
	b, _ := observability.NewBuffer(q, s, 10, 32768)
	guard, _ := observability.NewGuard(s, b)
	final, _ := sourceFinal(t, ingest.Full, []string{`{"XQH":"01","EXTRA":null}`}, guard)
	if _, err := b.Deliver(context.Background(), &recoverableLogs{down: true}); err != observability.ErrDelivery {
		t.Fatal("logs failure missing")
	}
	row, _, err := consumer.Consume(context.Background(), final)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt := control.Repository{Registry: r.Registry, Database: db}
	pending, err := rebuilt.PendingNotifications(context.Background(), 10)
	if err != nil || len(pending) != 1 || pending[0].ID != row.ID {
		t.Fatal("outbox depended on log PG or in-memory consumer")
	}
}
func TestNotificationNamesAreBoundedAndSecretsAndMarkupAreRedacted(t *testing.T) {
	_, _, s, consumer := notificationFixture(t)
	rowJSON := `{"XQH":"01","TOPSECRET_TOKEN":null,"<img src=x>":null,"line\r\nbreak":null,"NORMAL_FIELD":null}`
	final, _ := sourceFinal(t, ingest.Full, []string{rowJSON}, nil)
	row, _, err := consumer.Consume(context.Background(), final)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"TOPSECRET_TOKEN", "<img", "line\\r", "fictional-secret-token"} {
		if strings.Contains(string(row.Payload), secret) {
			t.Fatal("unsafe field name reached notice")
		}
	}
	var payload notification.Payload
	_ = json.Unmarshal([]byte(row.Payload), &payload)
	found := false
	for _, field := range payload.Notice.Fields {
		if field.Name == "NORMAL_FIELD" {
			found = true
		}
		if field.Name == "" && len(field.Reference) != 64 {
			t.Fatal("redacted field cannot be matched")
		}
	}
	if !found {
		t.Fatal("safe schema name lost")
	}
	name, ref := s.SchemaField("TOPSECRET_TOKEN")
	if name != "" || ref != s.Fingerprint("schema", "TOPSECRET_TOKEN") {
		t.Fatal("schema reference unstable")
	}
}
func TestOutboxPersistenceFailureIsReturnedAndFrozenTimeSurvivesLogAckFailure(t *testing.T) {
	_, db, _, consumer := notificationFixture(t)
	final, err := sourceFinal(t, ingest.Full, []string{`{"XQH":"01","EXTRA":null}`}, events.SinkFunc(func(_ context.Context, e events.Event) error {
		if e.Phase == events.Finished {
			return errors.New("logs unavailable")
		}
		return nil
	}))
	if err == nil || final == nil || final.OccurredAt.IsZero() {
		t.Fatal("log failure erased final source fact")
	}
	at := final.OccurredAt
	db.failCommit = true
	if _, _, err := consumer.Consume(context.Background(), final); err != notification.ErrOutbox || len(db.rows) != 0 {
		t.Fatal("outbox write failure hidden")
	}
	db.failCommit = false
	row, _, err := consumer.Consume(context.Background(), final)
	if err != nil || !row.CreatedAt.Equal(at.Truncate(time.Microsecond)) {
		t.Fatal("replay recomputed final occurrence time")
	}
	var body notification.Payload
	_ = json.Unmarshal([]byte(row.Payload), &body)
	if !body.Notice.OccurredAt.Equal(at) {
		t.Fatal("payload lost original nanosecond fact")
	}
}
func TestUnknownFieldNamesCannotSmugglePersonnelIdentifiers(t *testing.T) {
	for _, value := range []string{"null", `"Person-Fictional"`} {
		r, _, s, _ := notificationFixture(t)
		consumer, err := notification.NewConsumer(r, s, "test")
		if err != nil {
			t.Fatal(err)
		}
		final, _ := sourceFinal(t, ingest.Full, []string{`{"XQH":"01","Person_Fictional":` + value + `,"Student00019":` + value + `}`}, nil)
		row, _, err := consumer.Consume(context.Background(), final)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"Person_Fictional", "Student00019", "Person-Fictional"} {
			if strings.Contains(string(row.Payload), secret) {
				t.Fatal("personnel identifier in unknown field name leaked")
			}
		}
		var p notification.Payload
		_ = json.Unmarshal([]byte(row.Payload), &p)
		if p.Notice.UnknownCount != 2 || len(p.Notice.Fields) != 2 || p.Notice.Fields[0].Name != "" || p.Notice.Fields[1].Name != "" {
			t.Fatal("default unknown-name privacy boundary missing")
		}
	}
}
func TestNoticeTruncationRetainsFullFailureFingerprint(t *testing.T) {
	_, _, _, consumer := notificationFixture(t)
	record := map[string]any{"XQH": "01"}
	for i := 0; i < 70; i++ {
		record[fmt.Sprintf("FIELD_%03d", i)] = nil
	}
	raw, _ := json.Marshal(record)
	final, _ := sourceFinal(t, ingest.Full, []string{string(raw)}, nil)
	row, _, err := consumer.Consume(context.Background(), final)
	if err != nil {
		t.Fatal(err)
	}
	var p notification.Payload
	_ = json.Unmarshal([]byte(row.Payload), &p)
	if len(row.Payload) > 16384 || len(p.Notice.Fields) != 64 || !p.Notice.Truncated || p.Notice.UnknownCount != 70 {
		t.Fatal("unbounded or silent notification truncation")
	}
	changed := *final
	changed.UnknownFields = append([]string(nil), final.UnknownFields...)
	changed.UnknownFields[69] = "FIELD_999"
	if _, _, err := consumer.Consume(context.Background(), &changed); err != notification.ErrConflict {
		t.Fatal("omitted field changed without conflict")
	}
}
