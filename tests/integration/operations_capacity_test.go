//go:build integration && linux

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type oiCapacity struct {
	Records     int     `json:"records_per_source"`
	EventBytes  int     `json:"event_bytes"`
	EventMax    int     `json:"event_max_bytes"`
	Rate        int     `json:"write_rate"`
	History     int     `json:"history_clients"`
	Streams     int     `json:"sse_clients"`
	Duration    int     `json:"duration_seconds"`
	P95         float64 `json:"p95_ms"`
	Memory      uint64  `json:"max_memory_bytes"`
	Backlog     int64   `json:"max_backlog"`
	ErrorRate   float64 `json:"max_error_rate"`
	Connections int     `json:"max_db_connections"`
	Machine     struct {
		CPUs   int    `json:"min_cpus"`
		Memory uint64 `json:"min_memory_bytes"`
	} `json:"machine"`
}

func capacityBaseline(t *testing.T) oiCapacity {
	t.Helper()
	raw, err := os.ReadFile(os.Getenv("SICP_OI_CAPACITY"))
	if err != nil {
		t.Fatal(err)
	}
	var c oiCapacity
	if json.Unmarshal(raw, &c) != nil || c.Records < 1 || c.Rate < 1 || c.Duration < 1 || c.History < 1 || c.Streams < 1 {
		t.Fatal("invalid frozen capacity input")
	}
	mem, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		t.Fatal(err)
	}
	var actual uint64
	for _, line := range strings.Split(string(mem), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			actual, _ = strconv.ParseUint(fields[1], 10, 64)
			actual *= 1024
		}
	}
	if runtime.NumCPU() < c.Machine.CPUs || actual < c.Machine.Memory {
		t.Fatal("machine below frozen baseline")
	}
	return c
}
func TestOI07_01(t *testing.T) {
	oiEvidence(t)
	c := capacityBaseline(t)
	f := newOILogs(t)
	safeEvent := func(id string, source storage.LogSource) observability.SafeEvent {
		kind := events.Request
		identity := events.Identity{RequestID: id}
		if source == storage.UpstreamLog {
			kind = events.Call
			identity = events.Identity{TaskID: "capacity-task", CallID: id, Purpose: events.SyncPurpose}
		}
		e := events.Event{ID: id, EntityID: id, Kind: kind, Phase: events.Started, Identity: identity, OccurredAt: time.Now().UTC().Truncate(time.Microsecond), Metadata: events.Metadata{Source: "dwd_bzkscjzlb", Path: "/apizt/openapi/invoke/sjzc/release/dwd_bzkscjzlb", Mode: "full", Page: 1, PageSize: 100}}
		safe, err := f.sanitize.Sanitize(e, observability.Details{})
		if err != nil || len(safe.Bytes()) < c.EventBytes || len(safe.Bytes()) > c.EventMax {
			t.Fatal("event outside frozen size band", len(safe.Bytes()), err)
		}
		return safe
	}
	stores := map[storage.LogSource]observability.Repository{}
	for _, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		store := observability.Repository{Registry: f.e.registry, Database: storage.VerifiedLogs{Registry: f.e.registry, Database: f.e.db}, Source: source}
		stores[source] = store
		for start := 0; start < c.Records; start += 100 {
			batch := []observability.SafeEvent{}
			for i := start; i < start+100 && i < c.Records; i++ {
				batch = append(batch, safeEvent(fmt.Sprintf("seed-%s-%d", source, i), source))
			}
			if err := store.StoreBatch(f.e.ctx, batch); err != nil {
				t.Fatal(err)
			}
		}
	}
	page := f.page(t, 0, storage.PlatformLog, "mode=live")
	upstreamPage := f.page(t, 1, storage.UpstreamLog, "mode=live")
	ctx, cancel := context.WithTimeout(f.e.ctx, time.Duration(c.Duration)*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	var written, observed, errors, reads atomic.Int64
	latencies := []float64{}
	var mu sync.Mutex
	for i := 0; i < c.Streams; i++ {
		source, cursor := storage.PlatformLog, *page.StreamCursor
		if i%2 == 1 {
			source, cursor = storage.UpstreamLog, *upstreamPage.StreamCursor
		}
		frames, stop, code := f.streamSource(t, i%2, source, cursor, "")
		if code != 200 {
			t.Fatal(code)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer stop()
			for {
				select {
				case frame, ok := <-frames:
					if !ok {
						if ctx.Err() == nil {
							errors.Add(1)
						}
						return
					}
					if frame.Kind == "event" {
						observed.Add(1)
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	for i := 0; i < c.History; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					start := time.Now()
					source := "platform"
					if index%2 == 1 {
						source = "upstream"
					}
					r, _ := http.NewRequestWithContext(ctx, "GET", f.proxy[index%2].URL+"/admin/v1/logs/"+source+"/events?mode=live&page_size=50", nil)
					r.AddCookie(f.cookie)
					resp, err := http.DefaultClient.Do(r)
					if err != nil {
						if ctx.Err() == nil {
							errors.Add(1)
						}
						continue
					}
					_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20+1))
					resp.Body.Close()
					if err != nil || resp.StatusCode != 200 {
						if ctx.Err() == nil {
							errors.Add(1)
						}
					}
					reads.Add(1)
					mu.Lock()
					latencies = append(latencies, float64(time.Since(start).Microseconds())/1000)
					mu.Unlock()
				}
			}
		}(i)
	}
	var peak uint64
	var maxBacklog int64
	maxConnections := 0
	var rssPeak uint64
	probes := []*pgx.Conn{f.e.connection(t, 5), f.e.connection(t, 6)}
	// Schedule batches at a fixed rate, recording actual successes and lag.
	start := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	sequence := 0
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-ticker.C:
			for _, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
				batch := []observability.SafeEvent{}
				for range c.Rate / 10 {
					sequence++
					batch = append(batch, safeEvent(fmt.Sprintf("live-%s-%d", source, sequence), source))
				}
				if err := stores[source].StoreBatch(f.e.ctx, batch); err != nil {
					errors.Add(1)
				} else if source == storage.PlatformLog {
					written.Add(int64(len(batch)))
				}
			}
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			if memory.HeapAlloc > peak {
				peak = memory.HeapAlloc
			}
			status, err := os.ReadFile("/proc/self/status")
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(string(status), "\n") {
				if strings.HasPrefix(line, "VmRSS:") {
					fields := strings.Fields(line)
					rss, _ := strconv.ParseUint(fields[1], 10, 64)
					if rss*1024 > rssPeak {
						rssPeak = rss * 1024
					}
				}
			}
			connections := 0
			for _, probe := range probes {
				var n int
				if err := probe.QueryRow(f.e.ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()").Scan(&n); err != nil {
					t.Fatal(err)
				}
				connections += n
			}
			if connections > maxConnections {
				maxConnections = connections
			}
			lag := written.Load()*int64(c.Streams) - observed.Load()
			if lag > maxBacklog {
				maxBacklog = lag
			}
		}
	}
	cancel()
	wg.Wait()
	sort.Float64s(latencies)
	if len(latencies) == 0 {
		t.Fatal("empty capacity sample")
	}
	p50, p95 := latencies[len(latencies)/2], latencies[(len(latencies)-1)*95/100]
	report := map[string]any{"p50_ms": p50, "p95_ms": p95, "history_requests": reads.Load(), "errors": errors.Load(), "platform_written": written.Load(), "sse_observed": observed.Load(), "max_backlog": maxBacklog, "rss_peak_bytes": rssPeak, "heap_peak_bytes": peak, "db_connections": maxConnections, "duration_seconds": time.Since(start).Seconds(), "logical_cpus": runtime.NumCPU(), "records_per_source": c.Records}
	raw, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filepath.Join(os.Getenv("SICP_OI_OUTPUT"), "capacity-measurements.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if p95 > c.P95 || rssPeak > c.Memory || maxBacklog > c.Backlog || maxConnections > c.Connections || float64(errors.Load())/float64(reads.Load()) > c.ErrorRate || written.Load() < int64(c.Duration*c.Rate*9/10) {
		t.Fatal("frozen capacity target missed", string(raw))
	}
}
func TestOI07_02(t *testing.T) {
	oiEvidence(t)
	t.Run("overload_and_disconnect", TestOI03_04)
	t.Run("single_database_backlog_recovery", TestOI02_03)
}
func TestOI07_03(t *testing.T) {
	oiEvidence(t)
	t.Run("every_reference", TestT34_04PostgresRetirementProtectsEveryRegisteredDependency)
	t.Run("new_reference_after_preview", TestT34_04To06PostgresReferenceRetirementAndReceivedPrefix)
	t.Run("concurrent_retirement", TestT34_05PostgresReferenceAdmissionWaitsForCommittedRetirement)
	t.Run("old_snapshot", TestT34_05PostgresOldSnapshotCannotRegisterRetiredReference)
}
func TestOI07_04(t *testing.T) {
	oiEvidence(t)
	f := newOILogs(t)
	c := f.e.connection(t, 4)
	if _, err := c.Exec(f.e.ctx, `INSERT INTO control.tasks(id,idempotency_key,mode,status,requested_by,accepted_at,finished_at) VALUES('synthetic-task','synthetic-task','initialize','success_unchanged','synthetic',clock_timestamp(),clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	registry, err := f.e.registry.WithServiceLogAccess(true)
	if err != nil {
		t.Fatal(err)
	}
	repo := operations.Repository{Registry: registry, Database: f.e.db}
	m := operations.Maintenance{Repository: repo, LogDatabase: f.e.db, Owner: "cleanup", Correlate: f.sanitize.Correlation}
	jobs := []operations.Job{}
	for i, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		f.append(t, "old", source)
		if _, err = f.e.connection(t, 5+i).Exec(f.e.ctx, "UPDATE logs.events SET received_at=clock_timestamp()-interval '40 days'"); err != nil {
			t.Fatal(err)
		}
		p, err := m.Preview(f.e.ctx, source, "admin", time.Now())
		if err != nil || p.Count != 1 {
			t.Fatal(p, err)
		}
		j, err := m.Accept(f.e.ctx, "admin", string(source), operations.CleanupRequest{PreviewID: p.ID, Revision: p.Revision}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, j)
	}
	if err = m.Pump(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	// The second source fails its delete transaction; first source stays committed.
	upstream := f.e.connection(t, 6)
	if _, err = upstream.Exec(f.e.ctx, "ALTER TABLE logs.cleanup_receipts RENAME TO unavailable_cleanup_receipts"); err != nil {
		t.Fatal(err)
	}
	restored := false
	t.Cleanup(func() {
		if !restored {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := upstream.Exec(ctx, "ALTER TABLE logs.unavailable_cleanup_receipts RENAME TO cleanup_receipts"); err != nil {
				t.Error("restore source receipt table:", err)
			}
		}
	})
	if err = m.Pump(f.e.ctx); err == nil {
		t.Fatal("missing source receipt table hidden")
	}
	first, err := repo.Job(f.e.ctx, jobs[0].ID, "admin", "cleanup")
	if err != nil || first.Status != "succeeded" {
		t.Fatal("other source rolled back", first, err)
	}
	if _, err = upstream.Exec(f.e.ctx, "ALTER TABLE logs.unavailable_cleanup_receipts RENAME TO cleanup_receipts"); err != nil {
		t.Fatal(err)
	}
	restored = true
	if _, err = c.Exec(f.e.ctx, "UPDATE control.management_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	if err = m.Pump(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	t.Run("control_writeback_and_audit_recovery", TestT34_12And13PostgresDeleteCommitThenControlFailureRecoversOnce)
}
func TestOI07_05(t *testing.T) {
	oiEvidence(t)
	t.Run("concurrent_runtime", oiConcurrentRuntime)
	t.Run("all_business_auth", TestI10_Phase2AuthenticationAndInvalidSources)
	t.Run("cache_snapshot_publication", TestI09_Phase2CachePublicationAndOldSnapshots)
	t.Run("notification_log_outage", TestT33_18And19NotifierRebuildDynamicRecipientsAndUnavailableLogs)
}

func oiConcurrentRuntime(t *testing.T) {
	f := phase2EdgeRuntime(t, nil)
	now := time.Now().UTC().Truncate(time.Microsecond)
	safe, err := f.wr.Sanitizer.Sanitize(events.Event{ID: "retention-seed", EntityID: "retention-seed", Kind: events.Request, Phase: events.Started, Identity: events.Identity{RequestID: "retention-seed"}, OccurredAt: now}, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	logs := f.e.connection(t, 5)
	if _, err = appendProtocol(f.e.ctx, logs, safe); err != nil {
		t.Fatal(err)
	}
	if _, err = logs.Exec(f.e.ctx, "UPDATE logs.events SET received_at=clock_timestamp()-interval '40 days' WHERE event_id=$1", safe.ID()); err != nil {
		t.Fatal(err)
	}
	registry, err := f.e.registry.WithServiceLogAccess(true)
	if err != nil {
		t.Fatal(err)
	}
	repo := operations.Repository{Registry: registry, Database: f.e.db}
	maintenance := operations.Maintenance{Repository: repo, LogDatabase: f.e.db, Owner: "parallel", Correlate: f.wr.Sanitizer.Correlation}
	preview, err := maintenance.Preview(f.e.ctx, storage.PlatformLog, "admin", now)
	if err != nil || preview.Count != 1 {
		t.Fatal(preview, err)
	}
	job, err := maintenance.Accept(f.e.ctx, "admin", "parallel-cleanup", operations.CleanupRequest{PreviewID: preview.ID, Revision: preview.Revision}, now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.e.ctx)
	joined := make(chan struct{})
	failure := make(chan error, 1)
	var cycles atomic.Int64
	go func() {
		defer close(joined)
		for ctx.Err() == nil {
			if err := f.worker.Notifier.Pump(ctx); err != nil && ctx.Err() == nil {
				failure <- err
				return
			}
			if _, err := f.wr.Logs.Deliver(ctx, observability.Repository{Registry: f.wc.Databases, Database: f.wr.Database}); err != nil && ctx.Err() == nil {
				failure <- err
				return
			}
			if err := maintenance.Pump(ctx); err != nil && ctx.Err() == nil {
				failure <- err
				return
			}
			cycles.Add(1)
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	defer func() { cancel(); <-joined }()
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{
		{contract.StudentGrades, `{"student_number":"S001","academic_year":"all"}`}, {contract.BatchGrades, `{"student_numbers":["S001"],"academic_year":"all"}`},
		{contract.StudentTimetable, `{"student_number":"NO_SUCH_STUDENT","academic_year":"2026-2027","semester":"1"}`}, {contract.StaffTimetable, `{"staff_number":"NO_SUCH_TEACHER","academic_year":"2026-2027","semester":"1"}`},
		{contract.Organizations, `{}`}, {contract.Students, `{"student_number":"S001"}`}, {contract.Staff, `{"staff_number":"T001"}`},
		{contract.StaffTeachingClasses, phase2Teacher}, {contract.TeachingClassStudents, phase2Roster},
	} {
		f.queryOK(t, tc.op, tc.body, "")
	}
	deadline := time.Now().Add(5 * time.Second)
	for cycles.Load() == 0 {
		select {
		case err := <-failure:
			t.Fatal(err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("maintenance starved by queries")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-joined
	select {
	case err := <-failure:
		t.Fatal(err)
	default:
	}
	done, err := repo.Job(f.e.ctx, job.ID, "admin", "cleanup")
	if err != nil || done.Status != "succeeded" {
		t.Fatal("parallel cleanup failed", done, err)
	}
}
