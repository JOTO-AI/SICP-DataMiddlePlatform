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
