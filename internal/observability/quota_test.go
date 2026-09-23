package observability_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT05_11DiskFullRejectsBeforeClaimingPersistence(t *testing.T) {
	j := newJournal()
	j.failPut = true
	q := quota(t, j)
	b, err := observability.NewBuffer(q, sanitizer(t), 10, 32768)
	if err != nil {
		t.Fatal(err)
	}
	guard, _ := observability.NewGuard(sanitizer(t), b)
	if err := guard.Record(context.Background(), event()); err != observability.ErrCapacity || len(j.files) != 0 {
		t.Fatal("disk-full event reported durable")
	}
	report := q.Status()
	if report.Rejected != 1 || !report.Warning || !report.Unavailable {
		t.Fatal("capacity failure was not visible")
	}
	recorder, err := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(8), guard, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, span, err := recorder.Begin(context.Background(), events.Request, events.Identity{}, "", 0); !errors.Is(err, events.ErrRecord) || span != nil {
		t.Fatal("unlogged new work admitted")
	}
}
func TestT05_11IdempotentConfirmationDiskFullStillRaisesCapacityAlarm(t *testing.T) {
	j := newJournal()
	q := quota(t, j)
	b, err := observability.NewBuffer(q, sanitizer(t), 10, 32768)
	if err != nil {
		t.Fatal(err)
	}
	safe := appendEvent(t, b, 1)
	original := string(j.files[safe.ID()])
	j.failPut = true
	if err := b.Append(context.Background(), safe); err != observability.ErrCapacity {
		t.Fatal("idempotent fsync capacity failure was not classified")
	}
	if len(j.files) != 1 || string(j.files[safe.ID()]) != original {
		t.Fatal("existing immutable event changed")
	}
	r := q.Status()
	if r.Rejected != 1 || !r.Warning || !r.Unavailable {
		t.Fatal("idempotent confirmation skipped capacity alarm")
	}
}
func TestQuotaCountsOrphansAndAllowsIdempotentReplayWhenFull(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "orphan.tmp"), make([]byte, 4096), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := observability.OpenDirectory(path, testkit.FixedRandom(8), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	q, err := observability.NewQuota(files, observability.CapacityConfig{MaxBytes: 4096, MaxFiles: 100, WarningPercent: 80, Policy: observability.RejectNew})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := observability.NewBuffer(q, sanitizer(t), 10, 32768)
	safe, _ := sanitizer(t).Sanitize(event(), observability.Details{})
	if err := b.Append(ctx, safe); err != observability.ErrCapacity {
		t.Fatal("orphan bytes bypassed quota")
	}
	usage, err := files.Usage(ctx)
	if err != nil || usage.Files != 1 || usage.Bytes != 4096 {
		t.Fatal("capacity rejection created data")
	}
	j := newJournal()
	q, err = observability.NewQuota(j, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 2, WarningPercent: 80, Policy: observability.RejectNew})
	if err != nil {
		t.Fatal(err)
	}
	b, _ = observability.NewBuffer(q, sanitizer(t), 10, 32768)
	if err := b.Append(ctx, safe); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(ctx, safe); err != nil {
		t.Fatal("full quota rejected existing immutable event")
	}
	e := event()
	e.ID = "another"
	other, _ := sanitizer(t).Sanitize(e, observability.Details{})
	if err := b.Append(ctx, other); err != observability.ErrCapacity || len(j.files) != 1 {
		t.Fatal("full quota admitted new event")
	}
}
func TestSharedQuotaSerializesParallelBufferAppends(t *testing.T) {
	j := newJournal()
	q, err := observability.NewQuota(j, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 2, WarningPercent: 80, Policy: observability.RejectNew})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b, _ := observability.NewBuffer(q, sanitizer(t), 10, 32768)
			e := event()
			e.ID = fmt.Sprintf("parallel-%d", n)
			safe, err := sanitizer(t).Sanitize(e, observability.Details{})
			if err != nil {
				results <- err
				return
			}
			results <- b.Append(context.Background(), safe)
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if err != observability.ErrCapacity {
			t.Fatal(err)
		}
	}
	if success != 1 || len(j.files) != 1 || q.Status().Rejected != 15 {
		t.Fatal("concurrent quota oversubscription")
	}
}
