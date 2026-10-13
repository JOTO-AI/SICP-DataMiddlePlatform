//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"github.com/jackc/pgx/v5"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOI01_01(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	one, two := f.e.connection(t, 5), f.e.connection(t, 6)
	var first, second string
	var oid1, oid2 uint32
	for i, c := range []*pgx.Conn{one, two} {
		var id string
		var oid uint32
		if err := c.QueryRow(f.e.ctx, "SELECT system_identifier::text,(SELECT oid FROM pg_database WHERE datname=current_database()) FROM pg_control_system()").Scan(&id, &oid); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first, oid1 = id, oid
		} else {
			second, oid2 = id, oid
		}
	}
	if first != second || oid1 == oid2 {
		t.Fatal("not separate databases on same cluster")
	}
	for i, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		id := f.append(t, "same-id", source)
		p := f.page(t, i, source, "mode=live")
		if len(p.Items) != 1 || p.Items[0].EventID != id || p.HighWater != 1 {
			t.Fatal("wrong source", p)
		}
	}
	if f.page(t, 0, storage.PlatformLog, "mode=live").Epoch == f.page(t, 1, storage.UpstreamLog, "mode=live").Epoch {
		t.Fatal("shared epoch")
	}
	if _, err := two.Exec(f.e.ctx, "UPDATE logs.deployment_identity SET source='platform'"); err == nil {
		t.Fatal("wrong source constraint missing")
	}
}
func TestOI01_02(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	old := newLogs(t, "")
	safe := f.event(t, "legacy-call", storage.UpstreamLog, time.Now().UTC().Truncate(time.Microsecond))
	if err := old.buffer.Append(f.e.ctx, safe); err != nil {
		t.Fatal(err)
	}
	// Explicit old-target replay preserves historical call rows in platform.
	capacity := observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 1000, WarningPercent: 80, Policy: observability.RejectNew}
	replay := observability.Repository{Registry: f.e.registry, Database: storage.VerifiedLogs{Registry: f.e.registry, Database: f.e.db}}
	if err := observability.PrepareDual(f.e.ctx, old.directory, old.sanitizer, capacity, replay); err == nil {
		t.Fatal("unmarked old buffer silently routed")
	}
	if err := old.directory.SetLegacyTarget(f.e.ctx, old.sanitizer.Fingerprint("legacy-log-target-v1", f.e.registry.LogTarget(storage.PlatformLog))); err != nil {
		t.Fatal(err)
	}
	if err := observability.PrepareDual(f.e.ctx, old.directory, old.sanitizer, capacity, replay); err != nil {
		t.Fatal(err)
	}
	if usage, err := old.directory.Usage(f.e.ctx); err != nil || usage.Files != 0 {
		t.Fatal("old writer not drained", usage, err)
	}
	f.append(t, "new-call", storage.UpstreamLog)
	p := f.page(t, 0, storage.PlatformLog, "mode=live")
	u := f.page(t, 0, storage.UpstreamLog, "mode=live")
	if len(p.Items) != 1 || p.Items[0].EventID != safe.ID() || len(u.Items) != 1 || u.Items[0].EventID == safe.ID() {
		t.Fatal("history reclassified", p, u)
	}
}
func TestOI01_03(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	for _, domain := range []migrate.Domain{migrate.Logs, migrate.UpstreamLogs} {
		plan, err := migrate.Catalog(domain)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := migrate.Apply(f.e.ctx, f.e.db, f.e.registry, migrate.Target{Domain: domain}, plan); err != nil || n != 0 {
			t.Fatal("migration replay", n, err)
		}
		plan[0].SQL += "\n-- incompatible"
		if _, err = migrate.Apply(f.e.ctx, f.e.db, f.e.registry, migrate.Target{Domain: domain}, plan[:1]); err == nil {
			t.Fatal("checksum accepted")
		}
	}
	t.Run("writer_privileges", TestT29_PostgresRolesRejectWriterBypassAndReaderMutation)
}
func TestOI01_04(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	f.append(t, "persist", storage.PlatformLog)
	before := f.page(t, 0, storage.PlatformLog, "mode=live")
	db, err := adapters.OpenPostgres(f.e.ctx, f.registry, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	reader := logquery.Repository{Registry: f.registry, Database: storage.VerifiedLogs{Registry: f.registry, Database: db}, Sanitizer: f.sanitize}
	page, err := reader.Read(f.e.ctx, logquery.Platform, logquery.ReadRequest{Filter: before.Filter})
	if err != nil || len(page.Records) != 1 || page.Snapshot.Epoch != before.Epoch {
		t.Fatal("restart lost persisted logs", page, err)
	}
	// Restoring an older database must invalidate pre-restore cursors.
	if _, err = f.e.connection(t, 5).Exec(f.e.ctx, "UPDATE logs.commit_state SET epoch=gen_random_uuid()::text"); err != nil {
		t.Fatal(err)
	}
	frames, stop, code := f.stream(t, 1, *before.StreamCursor, "")
	defer stop()
	_ = frames
	if code != 409 {
		t.Fatal("old epoch resumed", code)
	}
}
func TestOI02_01(t *testing.T) {
	oiEvidence(t)
	t.Run("ordered_transactions", TestT29_PostgresCounterCannotExposeLaterCommitPastPendingWriter)
}
func TestOI02_02(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	c := f.e.connection(t, 5)
	safe := f.event(t, "rollback", storage.PlatformLog, time.Now().UTC().Truncate(time.Microsecond))
	tx, err := c.Begin(f.e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = appendProtocol(f.e.ctx, tx, safe); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := appendProtocol(f.e.ctx, c, safe); err != nil || n != 1 {
		t.Fatal("rolled back allocation leaked", n, err)
	}
	if err = c.Close(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	c = f.e.connection(t, 5)
	if n, err := appendProtocol(f.e.ctx, c, safe); err != nil || n != 1 {
		t.Fatal("lost acknowledgement duplicated", n, err)
	}
	changed := f.event(t, "rollback", storage.PlatformLog, time.Now().UTC().Add(time.Second).Truncate(time.Microsecond))
	if n, err := appendProtocol(f.e.ctx, c, changed); err == nil && n != -1 {
		t.Fatal("payload conflict accepted", n)
	}
}
func TestOI02_03(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	for i, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		t.Run(string(source), func(t *testing.T) {
			c := f.e.connection(t, 5+i)
			name := c.Config().Database
			t.Cleanup(func() {
				if _, err := c.Exec(context.Background(), "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" ALLOW_CONNECTIONS true"); err != nil {
					t.Error(err)
				}
			})
			if _, err := c.Exec(f.e.ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" ALLOW_CONNECTIONS false"); err != nil {
				t.Fatal(err)
			}
			// Existing admin connection stays available only for fixture recovery.
			if _, err := c.Exec(f.e.ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()"); err != nil {
				t.Fatal(err)
			}
			buffer := newLogs(t, "")
			safe := f.event(t, "outage-"+string(source), source, time.Now().UTC().Truncate(time.Microsecond))
			if err := buffer.buffer.Append(f.e.ctx, safe); err != nil {
				t.Fatal(err)
			}
			store := observability.Repository{Registry: f.e.registry, Database: storage.VerifiedLogs{Registry: f.e.registry, Database: f.e.db}, Source: source}
			if _, err := buffer.buffer.Deliver(f.e.ctx, store); err == nil {
				t.Fatal("unavailable database acknowledged")
			}
			other := storage.UpstreamLog
			if source == other {
				other = storage.PlatformLog
			}
			f.append(t, "healthy-"+string(source), other)
			if _, err := c.Exec(f.e.ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" ALLOW_CONNECTIONS true"); err != nil {
				t.Fatal(err)
			}
			if _, err := buffer.buffer.Deliver(f.e.ctx, store); err != nil {
				t.Fatal(err)
			}
			if usage, err := buffer.directory.Usage(f.e.ctx); err != nil || usage.Files != 0 {
				t.Fatal("backlog retained after acknowledgement", usage, err)
			}
		})
	}
}
func TestOI02_04(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	seen := map[string]bool{}
	for _, id := range []string{"a", "b", "c"} {
		seen[f.append(t, id, storage.PlatformLog)] = true
	}
	f.append(t, "a", storage.UpstreamLog)
	p := f.page(t, 0, storage.PlatformLog, "mode=live&page_size=2")
	if p.NextCursor == nil || len(p.Items) != 2 {
		t.Fatal(p)
	}
	f.append(t, "after-snapshot", storage.PlatformLog)
	q := f.page(t, 1, storage.PlatformLog, "cursor="+url.QueryEscape(*p.NextCursor))
	if q.HighWater != p.HighWater || len(q.Items) != 1 {
		t.Fatal("snapshot changed", q)
	}
	for _, item := range append(p.Items, q.Items...) {
		if !seen[item.EventID] {
			t.Fatal("duplicate/unexpected", item.EventID)
		}
		delete(seen, item.EventID)
	}
	if len(seen) != 0 {
		t.Fatal("page gap")
	}
	resp, _ := f.get(t, 0, "/admin/v1/logs/upstream/events?cursor="+url.QueryEscape(*p.NextCursor), nil)
	if resp.StatusCode != 400 {
		t.Fatal("cross source cursor", resp.StatusCode)
	}
	if _, err := f.e.connection(t, 5).Exec(f.e.ctx, "UPDATE logs.events SET payload='{}' WHERE sequence=1"); err != nil {
		t.Fatal(err)
	}
	resp, raw := f.get(t, 0, "/admin/v1/logs/platform/events?mode=live", nil)
	if resp.StatusCode != 503 || strings.Contains(string(raw), `"items"`) {
		t.Fatal("corrupt partial success", resp.StatusCode, string(raw))
	}
}
func TestOI03_01(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	for _, id := range []string{"a", "b", "c"} {
		f.append(t, id, storage.PlatformLog)
	}
	p := f.page(t, 0, storage.PlatformLog, "mode=live&page_size=2")
	want := f.append(t, "between", storage.PlatformLog)
	frames, stop, code := f.stream(t, 0, *p.StreamCursor, "")
	defer stop()
	if code != 200 {
		t.Fatal(code)
	}
	event := nextOIFrame(t, frames, "event")
	var item logquery.EventItem
	if json.Unmarshal([]byte(event.Data), &item) != nil || item.EventID != want {
		t.Fatal("history-to-stream gap", event)
	}
	old := f.page(t, 1, storage.PlatformLog, "cursor="+url.QueryEscape(*p.NextCursor))
	if len(old.Items) != 1 || old.HighWater != 3 {
		t.Fatal("history moved with stream", old)
	}
}
func TestOI03_02(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	p := f.page(t, 0, storage.PlatformLog, "mode=live")
	f.append(t, "first", storage.PlatformLog)
	frames, stop, code := f.stream(t, 0, *p.StreamCursor, "")
	if code != 200 {
		t.Fatal(code)
	}
	first := nextOIFrame(t, frames, "event")
	stop()
	want := f.append(t, "late", storage.PlatformLog)
	frames, stop, code = f.stream(t, 1, *p.StreamCursor, first.ID)
	defer stop()
	if code != 200 {
		t.Fatal(code)
	}
	next := nextOIFrame(t, frames, "event")
	var item logquery.EventItem
	if json.Unmarshal([]byte(next.Data), &item) != nil || item.EventID != want || item.Sequence != 2 {
		t.Fatal("cross-instance continuation lost position", next)
	}
}
func TestOI03_03(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	p := f.page(t, 0, storage.PlatformLog, "mode=live")
	start := time.Now()
	frames, stop, code := f.stream(t, 0, *p.StreamCursor, "")
	defer stop()
	if code != 200 {
		t.Fatal(code)
	}
	nextOIFrame(t, frames, "checkpoint")
	if time.Since(start) > 5*time.Second {
		t.Fatal("proxy buffers initial checkpoint")
	}
	f.append(t, "low-rate", storage.PlatformLog)
	nextOIFrame(t, frames, "event")
	// The parser exposes a comment-only heartbeat as an empty frame.
	nextOIFrame(t, frames, "")
}
func TestOI03_04(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	p := f.page(t, 0, storage.PlatformLog, "mode=live")
	stops := []func(){}
	for range 16 {
		_, stop, code := f.stream(t, 0, *p.StreamCursor, "")
		if code != 200 {
			t.Fatal(code)
		}
		stops = append(stops, stop)
	}
	_, stop, code := f.stream(t, 0, *p.StreamCursor, "")
	stop()
	if code != 429 {
		t.Fatal("connection budget", code)
	}
	f.append(t, "independent-writer", storage.PlatformLog)
	for _, close := range stops {
		close()
	}
	// Cancellation releases handlers without waiting for the five-minute lifetime.
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, close, status := f.stream(t, 0, *p.StreamCursor, "")
		close()
		if status == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connection budget not released")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Run("slow_socket", oiSlowSocket)
}

func oiSlowSocket(t *testing.T) {
	f := newOILogs(t)
	p := f.page(t, 0, storage.PlatformLog, "mode=live")
	address, _ := url.Parse(f.proxy[0].URL)
	conn, err := net.DialTimeout("tcp", address.Host, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.(*net.TCPConn).SetReadBuffer(1024); err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	fmt.Fprintf(conn, "GET /admin/v1/logs/platform/stream?cursor=%s HTTP/1.1\r\nHost: %s\r\nCookie: %s=%s\r\n\r\n", url.QueryEscape(*p.StreamCursor), address.Host, f.cookie.Name, f.cookie.Value)
	response, err := http.ReadResponse(bufio.NewReaderSize(conn, 1024), nil)
	if err != nil || response.StatusCode != 200 {
		t.Fatal("slow client handshake", err)
	}
	// Hold and actively drain the other slots: only the slow socket may time out.
	var healthyEnded atomic.Int32
	for range 15 {
		frames, stop, code := f.stream(t, 0, *p.StreamCursor, "")
		if code != 200 {
			t.Fatal(code)
		}
		defer stop()
		go func() {
			for range frames {
			}
			healthyEnded.Add(1)
		}()
	}
	store := observability.Repository{Registry: f.e.registry, Database: storage.VerifiedLogs{Registry: f.e.registry, Database: f.e.db}, Source: storage.PlatformLog}
	for start := 0; start < 2000; start += 100 {
		batch := []observability.SafeEvent{}
		for i := start; i < start+100; i++ {
			batch = append(batch, f.event(t, fmt.Sprintf("slow-%d", i), storage.PlatformLog, time.Now().UTC().Truncate(time.Microsecond)))
		}
		if err := store.StoreBatch(f.e.ctx, batch); err != nil {
			t.Fatal("slow reader blocked ingestion", err)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, stop, code := f.stream(t, 0, *p.StreamCursor, "")
		stop()
		if code == 200 {
			if healthyEnded.Load() != 0 {
				t.Fatal("healthy clients were evicted")
			}
			break
		}
		if code != 429 || time.Now().After(deadline) {
			t.Fatal("slow write did not release connection budget", code)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func TestOI03_05(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	p := f.page(t, 0, storage.PlatformLog, "mode=live")
	_, stop, code := f.stream(t, 0, *p.StreamCursor, "tampered")
	stop()
	if code != 400 {
		t.Fatal("bad last event id fell back", code)
	}
	f.service[1].Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	_, expiredStop, expiredCode := f.stream(t, 1, *p.StreamCursor, "")
	expiredStop()
	if expiredCode != 409 {
		t.Fatal("expired cursor resumed", expiredCode)
	}
	frames, stop, code := f.stream(t, 0, *p.StreamCursor, "")
	defer stop()
	if code != 200 {
		t.Fatal(code)
	}
	r, _ := http.NewRequestWithContext(f.e.ctx, "POST", f.proxy[0].URL+"/console-api/logout", nil)
	r.AddCookie(f.cookie)
	r.Header.Set("X-SICP-Request", "console")
	r.Header.Set("X-SICP-CSRF", f.csrf)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-frames:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("revoked stream survived authorization budget")
		}
	}
}
