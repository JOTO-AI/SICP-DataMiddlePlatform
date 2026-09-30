package update_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

func TestInitializationContinuesAndSeparateTableRepairsReuseDurableSuccesses(t *testing.T) {
	testTableRepairs(t, false)
}

func TestForceFullContinuesAndSeparateTableRepairsReuseDurableSuccesses(t *testing.T) {
	testTableRepairs(t, true)
}

func testTableRepairs(t *testing.T, force bool) {
	t.Helper()
	acquire := func(a update.Acquirer) (*staging.Lease, error) {
		if !force {
			return a.Full(t.Context())
		}
		plan := updatePlan()
		plan.Mode, plan.Table = "force_full", a.OnlyTable
		return a.Acquire(t.Context(), plan, time.UTC)
	}
	dir, err := staging.OpenDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	quota, _ := staging.NewQuotaFiles(dir, 0)
	failed := map[string]bool{"dwd_bzkscjzlb": true, "dwd_bzksgrtxsjzlb": true}
	calls := map[string]int{}
	client, err := ingest.NewClient("https://mock.invalid", "synthetic", roundTrip(func(r *http.Request) (*http.Response, error) {
		name := path.Base(r.URL.Path)
		calls[name]++
		status := 200
		if failed[name] {
			status = 500
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":0,"list":[]}}`))}, nil
	}), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	clock := &timer{now: time.Date(2100, 1, 1, 8, 0, 0, 0, time.UTC)}
	collector, err := ingest.NewCoordinator(client, ingest.BudgetConfig{RequestTimeout: time.Second, PageTimeout: time.Minute, InitialBackoff: time.Second, MaxBackoff: time.Second, MaxAttempts: 1}, clock, testkit.FixedRandom(0), &cooldown{}, admission{})
	if err != nil {
		t.Fatal(err)
	}
	var progress []control.TableAcquisitionProgress
	makeRun := func(id, selected string, prior *staging.Store) (update.Acquirer, *staging.Store) {
		store, err := staging.NewStore(id, quota, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		budget, _ := ingest.NewTaskBudget(clock.Now().Add(time.Hour), 100)
		a := update.Acquirer{Collector: collector, Staging: store, Budget: budget, Limits: ingest.PageLimits{MaxPages: 100, MaxRecords: 1000}, PageSize: 2, Owner: id, Observe: func(err error) { t.Error(err) }, OnlyTable: selected, ContinueOnFailure: true, Progress: func(_ context.Context, p []control.TableAcquisitionProgress) error {
			progress = append([]control.TableAcquisitionProgress(nil), p...)
			return nil
		}}
		if prior != nil {
			a.Reuse = func(ctx context.Context, r ingest.PageRequest) (int64, error) {
				return prior.CopyTable(ctx, r.Table, store, r.Mode, a.Limits)
			}
		}
		return a, store
	}
	first, firstStore := makeRun("first", "", nil)
	if lease, err := acquire(first); err == nil || lease != nil {
		t.Fatal("incomplete initialization published")
	}
	if len(calls) != 14 {
		t.Fatal("one failure stopped other tables", calls)
	}
	for _, table := range progress {
		want := "collected"
		if failed[table.Table] {
			want = "failed"
		}
		if table.Status != want {
			t.Fatal(table)
		}
	}
	// Re-open the completed evidence as after a process restart.
	firstStore, err = staging.NewStore("first", quota, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	delete(failed, "dwd_bzkscjzlb")
	calls = map[string]int{}
	second, secondStore := makeRun("second", "dwd_bzkscjzlb", firstStore)
	if lease, err := acquire(second); err == nil || lease != nil {
		t.Fatal("other missing table was falsely complete")
	}
	if len(calls) != 1 || calls["dwd_bzkscjzlb"] != 1 {
		t.Fatal("repair fetched unrelated tables", calls)
	}
	delete(failed, "dwd_bzksgrtxsjzlb")
	calls = map[string]int{}
	third, _ := makeRun("third", "dwd_bzksgrtxsjzlb", secondStore)
	lease, err := acquire(third)
	if err != nil || lease == nil {
		t.Fatal("all repaired tables did not seal", err)
	}
	if len(calls) != 1 || calls["dwd_bzksgrtxsjzlb"] != 1 || lease.Validate(t.Context()) != nil {
		t.Fatal("completed table fetched again or invalid seal", calls)
	}
	reused := 0
	for _, table := range progress {
		if table.Reused {
			reused++
		}
	}
	if reused != 13 {
		t.Fatal("missing per-table reuse status", reused)
	}
}

func TestCancelledInitializationDoesNotKeepRequestingOtherTables(t *testing.T) {
	i, _, _, _ := initialization(t, "")
	a := i.Source.(update.Acquirer)
	a.ContinueOnFailure = true
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.Full(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPublishedTableCopyUsesCompleteCanonicalBaseline(t *testing.T) {
	i, task, _, calls := initialization(t, "")
	out, err := i.Run(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	baseline := i.Preparer.(candidate.TargetFiles)
	if err := baseline.Validate(t.Context(), out.Manifest); err != nil {
		t.Fatal(err)
	}
	dir, err := staging.OpenDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	q, _ := staging.NewQuotaFiles(dir, 0)
	target, _ := staging.NewStore("one-table", q, 1<<20)
	r := ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: ingest.ForceFull, Page: 1, PageSize: 2}
	pages, err := update.CopyBaselineTable(t.Context(), baseline, out.Manifest, r, target, ingest.PageLimits{MaxPages: 100, MaxRecords: 1000})
	if err != nil || pages != 2 {
		t.Fatal("baseline not copied as full pages", err, pages)
	}
	entries, err := target.Entries(t.Context())
	if err != nil || len(entries) != 2 || entries[0].Total != 3 || entries[1].Total != 3 {
		t.Fatal("baseline lost rows", err)
	}
	for _, entry := range entries {
		if entry.Request.Mode != ingest.ForceFull {
			t.Fatal("mixed update modes")
		}
	}
	*calls = nil
	a := i.Source.(update.Acquirer)
	a.Staging, err = staging.NewStore("published-repair", q, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	a.OnlyTable = "dwd_bzkscjzlb"
	a.Reuse = func(ctx context.Context, r ingest.PageRequest) (int64, error) {
		return update.CopyBaselineTable(ctx, baseline, out.Manifest, r, a.Staging, a.Limits)
	}
	plan := updatePlan()
	plan.Mode, plan.Table = "force_full", a.OnlyTable
	lease, err := a.Acquire(t.Context(), plan, time.UTC)
	if err != nil || lease == nil {
		t.Fatal("published table repair did not seal", err)
	}
	if len(*calls) != 1 || (*calls)[0] != a.OnlyTable+":1" {
		t.Fatal("published repair fetched unrelated tables", *calls)
	}
}
