package activity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHeartbeatRetainsStageAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	now := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	var snapshots []Snapshot
	tr := &tracker{ctx: ctx, now: func() time.Time { return now }, save: func(_ context.Context, p Snapshot) error { snapshots = append(snapshots, p); return nil }}
	tr.report("building_table", "dwd_bzkscjzlb", "")
	first := now
	now = now.Add(time.Minute)
	tr.pulse()
	if len(snapshots) != 2 || !snapshots[1].StartedAt.Equal(first) || !snapshots[1].HeartbeatAt.Equal(now) {
		t.Fatal(snapshots)
	}
	tr.report("writing_table", "dwd_bzkscjzlb", "M")
	if !snapshots[2].StartedAt.Equal(now) || snapshots[2].Instance != "M" {
		t.Fatal(snapshots)
	}
	cancel()
	tr.pulse()
	tr.report("publishing", "", "")
	if len(snapshots) != 3 {
		t.Fatal("cancelled task kept heartbeating")
	}
}

func TestTelemetryFailureIsObservedWithoutCancellingWork(t *testing.T) {
	observed := 0
	ctx, stop := Track(t.Context(), time.Now, func(context.Context, Snapshot) error { return errors.New("telemetry unavailable") }, func(error) { observed++ })
	Report(ctx, "sealing", "", "")
	stop()
	Report(ctx, "publishing", "", "")
	if observed != 1 || ctx.Err() != nil {
		t.Fatal("telemetry changed work lifecycle", observed, ctx.Err())
	}
}

func TestBT09_RealWorkRetryAndHeartbeatAreSeparate(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	now := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	var saved []Snapshot
	tr := &tracker{ctx: ctx, now: func() time.Time { return now }, save: func(_ context.Context, s Snapshot) error { saved = append(saved, s); return nil }}
	advance := tr.begin("writing_table", "dwd_bzkscjzlb", "M", "rows", "", 10)
	advance(10, 1)
	if len(saved) != 1 {
		t.Fatal("per-row control write")
	}
	progress := now
	now = now.Add(time.Minute)
	tr.pulse()
	s := saved[len(saved)-1]
	if s.Completed != 10 || s.Batches != 1 || s.LastProgressAt == nil || !s.LastProgressAt.Equal(progress) || s.Phase != "writing_table" {
		t.Fatal(s)
	}
	check := tr.begin("content_check", "dwd_bzkscjzlb", "S", "rows", "", 10)
	advance(100, 1) // A late writer may not contaminate S validation.
	check(2, 0)
	tr.pulse()
	if tr.latest.Completed != 2 || tr.latest.Batches != 0 {
		t.Fatal(tr.latest)
	}
	advance = tr.begin("writing_table", "dwd_bzkscjzlb", "M", "rows", "", 10)
	advance(3, 1)
	tr.pulse()
	if tr.latest.Attempt != 2 || tr.latest.Completed != 3 || tr.latest.CumulativeCompleted != 13 || tr.latest.CumulativeElapsedMS != 60000 {
		t.Fatal(tr.latest)
	}
	tr.report("publishing", "", "")
	if tr.latest.Completed != 0 || tr.latest.Unit != "" || tr.latest.LastProgressAt != nil {
		t.Fatal(tr.latest)
	}
	cancel()
	advance(1, 1)
	if tr.latest.Completed != 0 {
		t.Fatal("cancelled work advanced")
	}
}
