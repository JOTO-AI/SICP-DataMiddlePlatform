package events_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestBT01_04NestedStagesInheritCopyAndAggregateMetrics(t *testing.T) {
	r, saved, _ := recorder(t)
	root := events.WithBuildTrace(events.WithRecorder(t.Context(), r, events.Identity{TaskID: "task"}), 1, nil)
	for _, instance := range []string{"M", "S"} {
		ctx, end := events.TraceBuild(root, events.BuildScope{Stage: "write_copy", Combination: "C1", Candidate: "candidate" + instance, Slot: 1, Generation: 7}, instance)
		child, done := events.TraceBuild(ctx, events.BuildScope{Stage: "target_check"}, "")
		buildmetric.Add(child, buildmetric.Decodes, 3)
		done(nil)
		end(nil)
	}
	for i, e := range *saved {
		if e.Phase != events.Finished {
			continue
		}
		b := e.Metadata.Build
		want := "M"
		if i >= 4 {
			want = "S"
		}
		if e.Identity.Instance != want || b.Candidate != "candidate"+want || b.Combination != "C1" || b.Slot != 1 || b.Generation != 7 || b.Metrics.Decodes != 3 || b.Attempt != 1 {
			t.Fatal("child metrics lost physical scope")
		}
	}
}

func TestBT01_03BuildStartPersistenceHasBoundedContext(t *testing.T) {
	now := time.Now()
	called := false
	reported := false
	ctx := events.WithBuildStart(t.Context(), func() time.Time { return now }, func(c context.Context, at time.Time) error {
		called = true
		deadline, ok := c.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second || !at.Equal(now) || at.Location() != time.UTC {
			t.Fatal("invalid persisted start boundary")
		}
		return errors.New("private SQL")
	}, func(err error) { reported = err == events.ErrRecord })
	events.MarkBuildStarted(ctx)
	if !called || !reported || ctx.Err() != nil {
		t.Fatal("timing failure changed task context")
	}
}

func TestBT01_01BuildBoundariesAndRetryTotals(t *testing.T) {
	r, saved, clock := recorder(t)
	ctx := events.WithBuildTrace(events.WithRecorder(t.Context(), r, events.Identity{TaskID: "task"}), 7, nil)
	scope := events.BuildScope{Stage: "content_table", Table: "dwd_bzkscjzlb", Combination: "C1", Slot: 1, Generation: 3}
	first := clock.Time
	end := events.BeginBuild(ctx, scope, "M")
	clock.Time = clock.Time.Add(2 * time.Second)
	end(errors.New("private source value"))
	end(nil) // A repeated cleanup cannot rewrite failure as success.
	end = events.BeginBuild(ctx, scope, "M")
	clock.Time = clock.Time.Add(3 * time.Second)
	end(nil)
	if len(*saved) != 4 {
		t.Fatal("expected two immutable start/end pairs", len(*saved))
	}
	start, fail, retry, success := (*saved)[0], (*saved)[1], (*saved)[2], (*saved)[3]
	if !start.OccurredAt.Equal(first) || start.OccurredAt.Location() != time.UTC || fail.Outcome != events.Failed || success.Outcome != events.Succeeded {
		t.Fatal("incorrect boundary or outcome")
	}
	if *fail.DurationNS != int64(2*time.Second) || *success.DurationNS != int64(3*time.Second) || success.Metadata.Build.CumulativeNS != int64(5*time.Second) {
		t.Fatal("attempt and cumulative elapsed time were mixed")
	}
	if retry.Metadata.Build.Attempt != 2 || retry.Metadata.Build.CumulativeNS != int64(2*time.Second) || start.Metadata.Build.CumulativeNS != 0 || success.Metadata.Build.OwnerGeneration != 7 {
		t.Fatal("retry changed historical facts")
	}
	for _, e := range *saved {
		if e.Validate() != nil || e.Identity.TaskID != "task" {
			t.Fatal("invalid persisted event")
		}
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), "private source value") || strings.Contains(string(raw), "usable_at") {
			t.Fatal("timing disclosed source data or invented usability")
		}
	}
}

func TestBT01_02CancellationCannotRecordSuccess(t *testing.T) {
	for _, workErr := range []error{nil, context.Canceled, context.DeadlineExceeded} {
		r, saved, clock := recorder(t)
		ctx, cancel := context.WithCancel(events.WithBuildTrace(events.WithRecorder(t.Context(), r, events.Identity{TaskID: "task"}), 1, nil))
		end := events.BeginBuild(ctx, events.BuildScope{Stage: "publish"}, "")
		clock.Time = clock.Time.Add(time.Second)
		if workErr == nil {
			cancel()
		}
		end(workErr)
		cancel()
		if len(*saved) != 2 || (*saved)[1].Outcome != events.Interrupted {
			t.Fatal("cancelled work recorded as successful")
		}
	}
}

func TestBT01_03TelemetryFailureIsBoundedAndDoesNotChangeWork(t *testing.T) {
	var observed, calls atomic.Int32
	r, err := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), events.SinkFunc(func(ctx context.Context, _ events.Event) error {
		calls.Add(1)
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded telemetry write")
		}
		<-ctx.Done()
		return errors.New("private database credentials")
	}), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx := events.WithBuildTrace(events.WithRecorder(t.Context(), r, events.Identity{TaskID: "task"}), 1, func(err error) {
		if err != events.ErrRecord {
			t.Error("unsafe diagnostic")
		}
		observed.Add(1)
	})
	end := events.BeginBuild(ctx, events.BuildScope{Stage: "sealing"}, "")
	end(nil)
	if observed.Load() != 1 || calls.Load() != 1 || ctx.Err() != nil {
		t.Fatal("telemetry altered work or fabricated a terminal sample")
	}
}

func TestBT01_04ScopesAndRecoveredOwnersDoNotShareAttempts(t *testing.T) {
	r, saved, clock := recorder(t)
	parent := events.WithRecorder(t.Context(), r, events.Identity{TaskID: "task"})
	ctx := events.WithBuildTrace(parent, 1, nil)
	scope := events.BuildScope{Stage: "content_table", Table: "dwd_bzkscjzlb", Combination: "C1", Generation: 1}
	for _, instance := range []string{"M", "S"} {
		end := events.BeginBuild(ctx, scope, instance)
		clock.Time = clock.Time.Add(time.Second)
		end(nil)
	}
	scope.Table = "dwd_xqjbsjzlb"
	end := events.BeginBuild(ctx, scope, "S")
	end(nil)
	ctx = events.WithBuildTrace(parent, 2, nil)
	end = events.BeginBuild(ctx, scope, "S")
	end(nil)
	for _, e := range *saved {
		if e.Metadata.Build.Attempt != 1 {
			t.Fatal("scope inherited another attempt")
		}
	}
	if (*saved)[7].Metadata.Build.OwnerGeneration != 2 || (*saved)[7].Metadata.Build.CumulativeNS != 0 {
		t.Fatal("recovery inherited previous process duration")
	}
	// The worker can resume in the same process with the same fencing token.
	ctx = events.WithBuildTrace(parent, 2, nil)
	end = events.BeginBuild(ctx, scope, "S")
	end(nil)
	before, after := (*saved)[7].Metadata.Build, (*saved)[9].Metadata.Build
	if before.SessionID == after.SessionID || after.SessionID == "" || after.Attempt != 1 || after.CumulativeNS != 0 {
		t.Fatal("same-owner recovery cannot be distinguished from the prior session")
	}
}

func TestBT01_05ConcurrentStagesJoinWithoutDuplicateTerminalEvents(t *testing.T) {
	var mu sync.Mutex
	var saved []events.Event
	r, err := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), events.SinkFunc(func(_ context.Context, e events.Event) error {
		mu.Lock()
		defer mu.Unlock()
		saved = append(saved, e)
		return nil
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := events.WithBuildTrace(events.WithRecorder(t.Context(), r, events.Identity{TaskID: "task"}), 1, nil)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			end := events.BeginBuild(ctx, events.BuildScope{Stage: "merge_table", Table: "dwd_bzkscjzlb"}, "")
			end(nil)
			end(nil)
		}()
	}
	wg.Wait()
	if len(saved) != 64 {
		t.Fatal("lost or duplicate events", len(saved))
	}
	seen := map[int]bool{}
	for _, e := range saved {
		if e.Validate() != nil {
			t.Fatal("invalid concurrent event")
		}
		if e.Phase == events.Started {
			if seen[e.Metadata.Build.Attempt] {
				t.Fatal("duplicate attempt")
			}
			seen[e.Metadata.Build.Attempt] = true
		}
	}
}

func TestBT01_06RejectsUntrustedStageFieldsAndBackwardsClock(t *testing.T) {
	r, saved, clock := recorder(t)
	observed := 0
	ctx := events.WithBuildTrace(events.WithRecorder(t.Context(), r, events.Identity{TaskID: "task"}), 1, func(error) { observed++ })
	for _, scope := range []events.BuildScope{
		{Stage: "usable_at"},
		{Stage: "write_copy", Table: "student-name"},
		{Stage: "write_copy", Combination: "postgres://secret"},
		{Stage: "business_check", Operation: "arbitrary-operation"},
	} {
		events.BeginBuild(ctx, scope, "M")(nil)
	}
	if len(*saved) != 0 || observed != 4 {
		t.Fatal("invalid scope reached persistence")
	}
	end := events.BeginBuild(ctx, events.BuildScope{Stage: "sealing"}, "")
	clock.Time = clock.Time.Add(-time.Second)
	end(nil)
	if len(*saved) != 1 || observed != 5 {
		t.Fatal("negative duration became a successful timing sample")
	}
}
