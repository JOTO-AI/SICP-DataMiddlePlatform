//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildfixture"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/worker"
)

type buildEvidence struct {
	Version, Scale, GoVersion string
	Seed                      uint64
	Scenarios                 []buildScenario
	Stages                    []events.Event
	DiagnosticOnly            bool
	Configuration             map[string]int
}
type buildScenario struct {
	Fixture                                  buildfixture.Fixture
	Name, State                              string
	Expected                                 []buildfixture.Summary
	Timing                                   *control.BuildTiming
	PreparationNS, WorkerNS, BuildToUsableNS int64
}
type stageCapture struct {
	mu       sync.Mutex
	sink     events.Sink
	stages   []events.Event
	overflow bool
}

func (s *stageCapture) Record(ctx context.Context, e events.Event) error {
	if err := s.sink.Record(ctx, e); err != nil {
		return err
	}
	if e.Metadata.Build == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stages) >= 10000 {
		s.overflow = true
		return nil
	}
	// Copy the metadata because the production recorder owns its values.
	b := *e.Metadata.Build
	e.Metadata.Build = &b
	s.stages = append(s.stages, e)
	return nil
}

func TestI05_B01SmallRealWorkerBaseline(t *testing.T) { runBuildBaseline(t, true) }
func TestI05_B01UnconfirmedAndFailedProbeNeverMarkUsable(t *testing.T) {
	f := newRuntime(t, false)
	ctx := f.e.ctx
	id := "b01-probe-failure"
	if _, err := f.wr.Control.Admit(ctx, control.Request{ID: id, IdempotencyKey: id, Mode: "initialize", RequestedBy: "synthetic", AcceptedAt: realClock{}.Now()}); err != nil {
		t.Fatal(err)
	}
	launch, err := f.wr.Control.StartAccepted(ctx, id, f.wr.ProcessID, realClock{}.Now())
	if err != nil || !launch.Started {
		t.Fatal("task did not start")
	}
	repo := f.wr.Control.WithOwnership(launch.Owner)
	first := realClock{}.Now()
	if err = repo.StartBuildTiming(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err = repo.StartBuildTiming(ctx, first.Add(time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	before, err := repo.BuildTiming(ctx, id)
	if err != nil || before == nil || !before.BuildStartedAt.Equal(first) {
		t.Fatal("retry changed first boundary")
	}
	if err = repo.RecordBuildUsable(ctx, "unpublished", first); err == nil {
		t.Fatal("unconfirmed publication marked usable")
	}
	executor := f.worker.Engine.Work.(worker.Executor)
	executor.Config.Service.APIKey = "deliberately-wrong-synthetic-key"
	if err = executor.Run(ctx, launch.Task, launch.Owner, false, nil); err != nil {
		t.Fatal("probe observation changed successful task")
	}
	after, err := repo.BuildTiming(ctx, id)
	if err != nil || after == nil || after.UsableAt != nil || !after.BuildStartedAt.Equal(first) {
		t.Fatal("failed probe fabricated timing")
	}
	task := control.Task{ID: id}
	if err = f.wr.Control.Load(ctx, &task); err != nil || task.Status != "success_published" {
		t.Fatal("probe changed publication fact")
	}
}
func TestB01_Performance(t *testing.T) {
	if os.Getenv("SICP_BUILD_PERFORMANCE") != "1" {
		t.Skip("explicit isolated capacity workflow required")
	}
	runBuildBaseline(t, false)
}
func runBuildBaseline(t *testing.T, small bool) {
	t.Helper()
	budget := 100 * time.Minute
	if small {
		budget = 3 * time.Minute
	}
	f := newRuntimeOptions(t, false, budget, func(v map[string]string) { v["SOURCE_TIMEZONE"] = "UTC" })
	// Run the production log dispatchers during capacity acquisition, just as
	// deployed services do; otherwise the test alone fills the durable journal.
	for _, dispatcher := range []*observability.Dispatcher{f.worker.Logs, f.query.Logs} {
		logCtx, stop := context.WithCancel(f.e.ctx)
		done := make(chan error, 1)
		go func() { done <- dispatcher.Run(logCtx) }()
		defer func() {
			stop()
			if err := <-done; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Error("log dispatcher failed")
			}
		}()
	}
	report := buildEvidence{Version: buildfixture.Version, Scale: "capacity", Seed: 73, GoVersion: runtime.Version(), DiagnosticOnly: true}
	report.Configuration = map[string]int{"source_page_size": f.wc.SourcePageSize, "source_page_concurrency": f.wc.SourcePageConcurrency, "build_table_concurrency": f.wc.BuildTableConcurrency, "write_batch_rows": 500, "pool_max_per_database": 16}
	if small {
		report.Scale = "small"
	}
	dir := filepath.Join("..", "..", "artifacts", "build-performance", report.Scale)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	executor := f.worker.Engine.Work.(worker.Executor)
	capture := &stageCapture{sink: executor.Sink}
	recorder, err := events.New(ports.SystemClock{}, rand.Reader, capture, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	executor.Recorder = recorder
	f.worker.Engine.Work = executor
	defer func() {
		capture.mu.Lock()
		report.Stages = append([]events.Event(nil), capture.stages...)
		overflow := capture.overflow
		capture.mu.Unlock()
		if overflow {
			t.Error("stage evidence overflow")
		}
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		if err = os.WriteFile(filepath.Join(dir, "stages.json"), raw, 0600); err != nil {
			t.Error(err)
		}
	}()
	if !small && os.Getenv("SICP_BUILD_PROFILE") == "1" {
		file, err := os.Create(filepath.Join(dir, "cpu.pprof"))
		if err != nil {
			t.Fatal(err)
		}
		if err = pprof.StartCPUProfile(file); err != nil {
			file.Close()
			t.Fatal(err)
		}
		defer func() {
			pprof.StopCPUProfile()
			file.Close()
			heap, err := os.Create(filepath.Join(dir, "heap.pprof"))
			if err != nil {
				t.Error(err)
				return
			}
			defer heap.Close()
			if err = pprof.WriteHeapProfile(heap); err != nil {
				t.Error(err)
			}
		}()
	}
	// The real source collector persists pages. The recorded build start is
	// later, immediately before the first seal: acquisition stays outside T.
	epoch := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	fixture := buildfixture.Fixture{Seed: report.Seed, Small: small, Epoch: epoch}
	blocked := false
	for i, mode := range []string{"initialize", "incremental", "force_full"} {
		pct := []int{0, 1, 100}[i]
		fixture.Percent = pct
		if i > 0 {
			fixture.ChangedAt = time.Now().UTC().Add(-time.Microsecond)
		}
		scenario := buildScenario{Name: mode, State: "preparing", Fixture: fixture}
		started := time.Now()
		scenario.Expected, err = fixture.Expected(f.e.ctx, f.wc.SourceScope)
		scenario.PreparationNS = time.Since(started).Nanoseconds()
		if err != nil {
			scenario.State = "reference_failed"
			report.Scenarios = append(report.Scenarios, scenario)
			t.Fatal("independent reference failed")
		}
		var rows int
		var bytes int64
		for _, s := range scenario.Expected {
			rows += s.Rows
			bytes += s.Bytes
			if pct > 0 && s.Changed < 1 {
				t.Fatal("unchanged table in changed fixture")
			}
		}
		if !small && (rows != buildfixture.TotalRows || bytes < 480000000 || bytes > 600000000) {
			scenario.State = "capacity_mismatch"
			report.Scenarios = append(report.Scenarios, scenario)
			t.Fatalf("capacity mismatch: rows=%d bytes=%d", rows, bytes)
		}
		if blocked {
			scenario.State = "blocked_by_initial_baseline"
			report.Scenarios = append(report.Scenarios, scenario)
			continue
		}
		f.source.mu.Lock()
		f.source.generated = fixture.Handler("synthetic-source-token")
		f.source.mu.Unlock()
		id := "b01-" + mode
		_, err = f.wr.Control.Admit(f.e.ctx, control.Request{ID: id, IdempotencyKey: id, Mode: mode, RequestedBy: "synthetic", AcceptedAt: realClock{}.Now()})
		if err != nil {
			t.Fatal("baseline admission failed")
		}
		started = time.Now()
		workErr := f.worker.Engine.Cycle(f.e.ctx)
		scenario.WorkerNS = time.Since(started).Nanoseconds()
		diagnostic, cancel := context.WithTimeout(context.WithoutCancel(f.e.ctx), 15*time.Second)
		timing, timingErr := f.wr.Control.BuildTiming(diagnostic, id)
		cancel()
		scenario.Timing = timing
		scenario.State = "failed"
		if timingErr != nil || timing == nil {
			report.Scenarios = append(report.Scenarios, scenario)
			t.Fatal("missing persisted build boundary")
		}
		if workErr != nil {
			if !small && errors.Is(workErr, read.ErrBudget) && timing.UsableAt == nil {
				active, exists, e := f.wr.Active.Read(f.e.ctx)
				if e != nil || exists && active.TaskID == id {
					t.Fatal("failed candidate was published")
				}
				scenario.State = "known_background_budget_limit"
				blocked = true
				report.Scenarios = append(report.Scenarios, scenario)
				continue
			}
			report.Scenarios = append(report.Scenarios, scenario)
			t.Fatalf("real worker failed: %v", workErr)
		}
		if timing.UsableAt == nil || timing.UsableAt.Before(timing.BuildStartedAt) {
			report.Scenarios = append(report.Scenarios, scenario)
			t.Fatal("publication did not produce authenticated usable evidence")
		}
		scenario.BuildToUsableNS = timing.UsableAt.Sub(timing.BuildStartedAt).Nanoseconds()
		if countControl(t, f, "SELECT count(*) FROM control.query_snapshots") != 0 {
			t.Fatal("build probes retained pagination snapshots")
		}
		stored := control.Bundle{ID: timing.BundleID}
		err = f.wr.Control.Load(f.e.ctx, &stored)
		var manifest candidate.Manifest
		if err != nil || json.Unmarshal([]byte(stored.Tables), &manifest) != nil || manifest.Validate() != nil {
			t.Fatal("missing real target manifest")
		}
		byTable := map[string]buildfixture.Summary{}
		for _, s := range scenario.Expected {
			byTable[s.Table] = s
		}
		for _, v := range manifest.Tables {
			want := byTable[v.Table]
			if v.Rows != int64(want.Rows) || v.SHA256 != want.SHA256 {
				t.Fatal("target differs from independent reference")
			}
		}
		scenario.State = "usable"
		report.Scenarios = append(report.Scenarios, scenario)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	var sql, decodes, cases, buffer, disk int64
	stages := map[string]bool{}
	for _, e := range capture.stages {
		if e.Phase != events.Finished {
			continue
		}
		b := e.Metadata.Build
		stages[b.Stage] = true
		if b.Metrics.Invalid {
			t.Error("invalid stage counters")
		}
		sql += b.Metrics.SQL
		decodes += b.Metrics.Decodes
		cases += b.Metrics.Cases
		buffer = max(buffer, b.Metrics.BufferPeak)
		disk = max(disk, b.Metrics.DiskPeak)
	}
	if small && (sql == 0 || decodes == 0 || cases == 0 || buffer == 0 || disk == 0 || !stages["usable_probe"] || !stages["old_copy_check"] || !stages["drain"]) {
		t.Error("incomplete stage/counter coverage")
	}
	// No 30-minute assertion in B01: this is a diagnostic baseline, not B10.
}
