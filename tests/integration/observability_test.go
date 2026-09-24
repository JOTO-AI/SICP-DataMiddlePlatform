//go:build integration

package integration

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

const syntheticSecret = "synthetic-secret-never-persist"
const syntheticPerson = "SyntheticPrivatePerson_123"

type logFixture struct {
	path      string
	sanitizer *observability.Sanitizer
	directory *observability.Directory
	buffer    *observability.Buffer
	guard     *observability.Guard
}

func newLogs(t *testing.T, path string) *logFixture {
	t.Helper()
	if path == "" {
		path = t.TempDir()
	}
	s, err := observability.NewSanitizer(16384, 4096, []byte(strings.Repeat("s", 32)), []string{syntheticSecret})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := observability.OpenDirectory(path, rand.Reader, 16384)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	quota, err := observability.NewQuota(dir, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 1000, WarningPercent: 80, Policy: observability.RejectNew})
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := observability.NewBuffer(quota, s, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := observability.NewGuard(s, buffer)
	if err != nil {
		t.Fatal(err)
	}
	return &logFixture{path, s, dir, buffer, guard}
}
func (l *logFixture) record(t *testing.T, phase events.Phase) {
	t.Helper()
	at := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	outcome := events.Outcome("")
	if phase == events.Finished {
		at = at.Add(time.Minute)
		outcome = events.Succeeded
	}
	e := events.Event{ID: "event_" + string(phase), EntityID: "task", Kind: events.Task, Phase: phase, Identity: events.Identity{TaskID: "task"}, OccurredAt: at, Outcome: outcome}
	if err := l.guard.RecordDetails(context.Background(), e, observability.Details{Message: syntheticSecret, Fields: map[string]string{"person": syntheticPerson}}); err != nil {
		t.Fatal(err)
	}
}
func compose(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", append([]string{"compose", "-p", "sicp-integration", "-f", "../../deploy/compose.yaml"}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated compose %v: %v\n%s", args, err, output)
	}
}
func stopLogPG(t *testing.T) func() {
	t.Helper()
	var once sync.Once
	restore := func() { once.Do(func() { compose(t, "up", "-d", "--wait", "--wait-timeout", "60", "db-log") }) }
	t.Cleanup(restore)
	compose(t, "stop", "db-log")
	return restore
}
func deliverEventually(t *testing.T, e *environment, l *logFixture) {
	t.Helper()
	store := observability.Repository{Registry: e.registry, Database: e.db}
	for i := 0; i < 5; i++ {
		if _, err := l.buffer.Deliver(e.ctx, store); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("log delivery did not recover")
}
func assertEventCount(t *testing.T, e *environment, want int) {
	t.Helper()
	var got int
	if err := e.connection(t, 5).QueryRow(e.ctx, "SELECT count(*) FROM logs.events").Scan(&got); err != nil || got != want {
		t.Fatalf("events=%d want=%d error=%v", got, want, err)
	}
}

func TestI02_01(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	l := newLogs(t, "")
	restore := stopLogPG(t)
	l.record(t, events.Started)
	if n, err := l.buffer.Deliver(e.ctx, observability.Repository{Registry: e.registry, Database: e.db}); err == nil || n != 0 {
		t.Fatalf("outage delivery: %d %v", n, err)
	}
	if pending, err := l.buffer.Pending(e.ctx); err != nil || len(pending) != 1 {
		t.Fatalf("lost durable event: %v", err)
	}
	if err := l.directory.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := newLogs(t, l.path)
	restore()
	deliverEventually(t, e, restarted)
	assertEventCount(t, e, 1)
	if pending, err := restarted.buffer.Pending(e.ctx); err != nil || len(pending) != 0 {
		t.Fatalf("acknowledged event retained: %v", err)
	}
}

type lostAckDB struct{ storage.Database }
type lostAckTx struct{ storage.Transaction }

func (d lostAckDB) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := d.Database.Begin(ctx, c)
	if err != nil {
		return nil, err
	}
	return lostAckTx{tx}, nil
}
func (t lostAckTx) Commit(ctx context.Context) error {
	if err := t.Transaction.Commit(ctx); err != nil {
		return err
	}
	return errors.New("synthetic commit response loss")
}
func TestI02_02(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	l := newLogs(t, "")
	l.record(t, events.Started)
	store := observability.Repository{Registry: e.registry, Database: lostAckDB{e.db}}
	if _, err := l.buffer.Deliver(e.ctx, store); err == nil {
		t.Fatal("unknown commit acknowledged")
	}
	assertEventCount(t, e, 1)
	if pending, err := l.buffer.Pending(e.ctx); err != nil || len(pending) != 1 {
		t.Fatalf("removed before ack: %v", err)
	}
	deliverEventually(t, e, l)
	assertEventCount(t, e, 1)
	if pending, err := l.buffer.Pending(e.ctx); err != nil || len(pending) != 0 {
		t.Fatalf("replay not acknowledged: %v", err)
	}
}
func TestI02_03(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	l := newLogs(t, "")
	store := observability.Repository{Registry: e.registry, Database: e.db}
	l.record(t, events.Finished)
	deliverEventually(t, e, l)
	before, err := store.ReadProjection(e.ctx, l.sanitizer, "task")
	if err != nil || before.Business != events.Succeeded {
		t.Fatalf("end projection: %v", err)
	}
	l.record(t, events.Started)
	deliverEventually(t, e, l)
	after, err := store.ReadProjection(e.ctx, l.sanitizer, "task")
	wantStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := wantStart.Add(time.Minute)
	if err != nil || after.Business != events.Succeeded || after.StartedAt == nil || after.FinishedAt == nil || !after.StartedAt.Equal(wantStart) || !after.FinishedAt.Equal(wantEnd) || !before.FinishedAt.Equal(*after.FinishedAt) {
		t.Fatalf("history changed: %v", err)
	}
}

// The test owns an actual loopback SMTP receiver. It is the external provider
// fixture; NewMail, Sender, Consumer and PG persistence are production code.
type mailReceiver struct {
	listener net.Listener
	fail     atomic.Bool
	mu       sync.Mutex
	messages []string
	done     chan struct{}
}

func newReceiver(t *testing.T) *mailReceiver {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &mailReceiver{listener: listener, done: make(chan struct{})}
	t.Cleanup(func() { listener.Close(); <-m.done })
	go func() {
		defer close(m.done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			m.receive(conn)
		}
	}()
	return m
}
func (m *mailReceiver) receive(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	fmt.Fprint(c, "220 synthetic\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
			fmt.Fprint(c, "250 synthetic\r\n")
		case strings.HasPrefix(line, "DATA"):
			fmt.Fprint(c, "354 data\r\n")
			var body strings.Builder
			for {
				line, err = r.ReadString('\n')
				if err != nil {
					return
				}
				if line == ".\r\n" {
					break
				}
				if body.Len()+len(line) > 1<<20 {
					return
				}
				body.WriteString(line)
			}
			if m.fail.Load() {
				fmt.Fprint(c, "451 synthetic failure\r\n")
			} else {
				m.mu.Lock()
				m.messages = append(m.messages, body.String())
				m.mu.Unlock()
				fmt.Fprint(c, "250 accepted\r\n")
			}
		case strings.HasPrefix(line, "QUIT"):
			fmt.Fprint(c, "221 bye\r\n")
			return
		default:
			fmt.Fprint(c, "502 unsupported\r\n")
		}
	}
}
func (m *mailReceiver) captured() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.messages...)
}
func newSender(t *testing.T, r control.Repository, s *observability.Sanitizer, m *mailReceiver) *notification.Sender {
	t.Helper()
	f, err := os.Open("../../deploy/examples/worker.env.example")
	if err != nil {
		t.Fatal(err)
	}
	values, err := config.ReadEnv(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	values["MAIL_ADDRESS"] = m.listener.Addr().String()
	c, err := config.LoadRuntime(config.Worker, func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	channel, err := adapters.NewMail(c)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := notification.NewSender(r, channel, realClock{}, s, notification.DeliveryConfig{From: c.MailFrom, To: c.MailTo, MaxAttempts: 3, Batch: 10, RetryDelay: time.Microsecond, SendTimeout: time.Second, StoreTimeout: time.Second, Lease: 4 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return sender
}
func unknownFetch(t *testing.T, e *environment, l *logFixture, recover bool) ingest.PageResult {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.URL.Path != "/apizt/openapi/invoke/sjzc/release/dwd_xqjbsjzlb" || r.URL.Query().Get("access_token") != syntheticSecret {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if recover && n > 1 {
			fmt.Fprint(w, `{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":"001"}]}}`)
		} else {
			fmt.Fprintf(w, `{"code":200,"message":"%s","data":{"total":1,"list":[{"XQH":"001","%s":"%s"}]}}`, syntheticSecret, syntheticPerson, syntheticPerson)
		}
	}))
	t.Cleanup(server.Close)
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	client, err := ingest.NewClient(server.URL, syntheticSecret, transport, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := events.New(realClock{}, rand.Reader, l.guard, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := events.WithRecorder(e.ctx, recorder, events.Identity{TaskID: "task"})
	result, err := client.FetchPage(ctx, ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: ingest.Full, Page: 1, PageSize: 10}, ingest.RetryPolicy{MaxAttempts: 3, Wait: func(context.Context, int) error { return nil }})
	want := int32(3)
	if recover {
		want = 2
	}
	if calls.Load() != want || result.ObservationError != nil {
		t.Fatalf("retry count=%d observation=%v", calls.Load(), result.ObservationError)
	}
	if recover {
		if err != nil || result.FinalFailure != nil {
			t.Fatalf("recovery: %v", err)
		}
	} else if err == nil || result.FinalFailure == nil {
		t.Fatal("missing final failure")
	}
	return result
}
func enqueue(t *testing.T, e *environment, l *logFixture, r control.Repository, recover bool) control.Notification {
	t.Helper()
	result := unknownFetch(t, e, l, recover)
	consumer, err := notification.NewConsumer(r, l.sanitizer, "test")
	if err != nil {
		t.Fatal(err)
	}
	row, created, err := consumer.Consume(e.ctx, result.FinalFailure)
	if err != nil || created == recover {
		t.Fatalf("notification persistence: created=%v error=%v", created, err)
	}
	return row
}
func TestI02_04(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := taskFixture(t, e)
	l := newLogs(t, "")
	row := enqueue(t, e, l, r, false)
	m := newReceiver(t)
	if n, err := newSender(t, r, l.sanitizer, m).Pump(e.ctx); err != nil || n != 1 {
		t.Fatalf("send: %d %v", n, err)
	}
	captured := m.captured()
	if len(captured) != 1 || !strings.Contains(captured[0], row.FailureKey) || strings.Contains(captured[0], syntheticPerson) || strings.Contains(captured[0], syntheticSecret) {
		t.Fatal("missing or unsafe mail")
	}
}
func TestI02_05(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := taskFixture(t, e)
	l := newLogs(t, "")
	enqueue(t, e, l, r, true)
	m := newReceiver(t)
	if n, err := newSender(t, r, l.sanitizer, m).Pump(e.ctx); err != nil || n != 0 || len(m.captured()) != 0 {
		t.Fatalf("recovered request notified: %d %v", n, err)
	}
	var count int
	if err := e.connection(t, 4).QueryRow(e.ctx, "SELECT count(*) FROM control.notification_outbox").Scan(&count); err != nil || count != 0 {
		t.Fatalf("unexpected outbox: %d %v", count, err)
	}
}
func TestI02_06(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := taskFixture(t, e)
	l := newLogs(t, "")
	row := enqueue(t, e, l, r, false)
	m := newReceiver(t)
	m.fail.Store(true)
	if n, err := newSender(t, r, l.sanitizer, m).Pump(e.ctx); err == nil || n != 0 {
		t.Fatal("failed SMTP accepted")
	}
	loaded := control.Notification{ID: row.ID}
	if err := r.Load(e.ctx, &loaded); err != nil || loaded.Status != "pending" || loaded.Attempts != 1 {
		t.Fatalf("retry fact missing: %v", err)
	}
	m.fail.Store(false)
	// Discard sender/repository state and reconstruct from the fixed database.
	restarted := control.Repository{Registry: e.registry, Database: e.db}
	if n, err := newSender(t, restarted, l.sanitizer, m).Pump(e.ctx); err != nil || n != 1 {
		t.Fatalf("restart retry: %d %v", n, err)
	}
	if err := restarted.Load(e.ctx, &loaded); err != nil || loaded.Status != "accepted" || loaded.Attempts != 2 || len(m.captured()) != 1 {
		t.Fatalf("acceptance not persisted: %v", err)
	}
}
func TestI02_07(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := taskFixture(t, e)
	l := newLogs(t, "")
	restore := stopLogPG(t)
	row := enqueue(t, e, l, r, false)
	restarted := control.Repository{Registry: e.registry, Database: e.db}
	pending, err := restarted.PendingNotifications(e.ctx, 10)
	if err != nil || len(pending) != 1 || pending[0].ID != row.ID {
		t.Fatalf("log outage lost notice: %v", err)
	}
	restore()
}
func TestI02_08(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	r := taskFixture(t, e)
	l := newLogs(t, "")
	l.record(t, events.Started)
	enqueue(t, e, l, r, false)
	check := func(body string) {
		t.Helper()
		for _, secret := range []string{syntheticSecret, syntheticPerson} {
			if strings.Contains(body, secret) {
				t.Fatal("synthetic sensitive value persisted")
			}
		}
	}
	files, err := os.ReadDir(l.path)
	if err != nil || len(files) == 0 {
		t.Fatalf("missing files: %v", err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(l.path, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		check(string(raw))
		if !json.Valid(raw) {
			t.Fatal("invalid durable JSON")
		}
	}
	deliverEventually(t, e, l)
	rows, err := e.connection(t, 5).Query(e.ctx, "SELECT payload::text FROM logs.events")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			t.Fatal(err)
		}
		check(body)
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || count == 0 {
		t.Fatalf("missing stored events: %v", err)
	}
	m := newReceiver(t)
	if n, err := newSender(t, r, l.sanitizer, m).Pump(e.ctx); err != nil || n != 1 {
		t.Fatalf("send: %d %v", n, err)
	}
	for _, body := range m.captured() {
		check(body)
	}
}
