package buildmetric_test

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
)

func TestBT01_04ConcurrentScopesAggregateWithoutMixingCopies(t *testing.T) {
	root, total := buildmetric.Begin(context.Background())
	m, mc := buildmetric.Begin(root)
	s, sc := buildmetric.Begin(root)
	var wg sync.WaitGroup
	for _, ctx := range []context.Context{m, s} {
		wg.Add(1)
		go func(ctx context.Context) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				buildmetric.Add(ctx, buildmetric.RowsWritten, 1)
				buildmetric.Memory(ctx, 12)
				buildmetric.Memory(ctx, -12)
			}
		}(ctx)
	}
	wg.Wait()
	if mc.Snapshot().RowsWritten != 1000 || sc.Snapshot().RowsWritten != 1000 || total.Snapshot().RowsWritten != 2000 {
		t.Fatal("counter scope isolation")
	}
	buildmetric.Memory(m, 20)
	buildmetric.Memory(s, 30)
	buildmetric.Memory(m, -20)
	buildmetric.Memory(s, -30)
	if total.Snapshot().BufferPeak != 50 || mc.Snapshot().BufferPeak != 20 || sc.Snapshot().BufferPeak != 30 {
		t.Fatal("live peaks must overlap, not sum historical peaks")
	}
	buildmetric.Disk(m, 70)
	buildmetric.Disk(m, -70)
	buildmetric.Disk(s, 40)
	buildmetric.Disk(s, -40)
	if total.Snapshot().DiskPeak != 70 {
		t.Fatal("disk peak inflated")
	}
}
func TestBT01_05CountersRemainSynchronousAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ctx, s := buildmetric.Begin(ctx)
	cancel()
	buildmetric.Add(ctx, buildmetric.RowsRead, 2)
	buildmetric.Memory(ctx, 8)
	buildmetric.Memory(ctx, -8)
	if s.Snapshot().RowsRead != 2 || s.Snapshot().Invalid {
		t.Fatal("final accounting lost")
	}
}
func TestBT01_06InvalidCountersNeverWrap(t *testing.T) {
	for _, bad := range []func(context.Context){func(c context.Context) { buildmetric.Add(c, buildmetric.SQL, -1) }, func(c context.Context) {
		buildmetric.Add(c, buildmetric.SQL, math.MaxInt64)
		buildmetric.Add(c, buildmetric.SQL, 1)
	}, func(c context.Context) { buildmetric.Memory(c, -1) }, func(c context.Context) { buildmetric.Disk(c, math.MinInt64) }, func(c context.Context) { buildmetric.Add(c, 255, 1) }} {
		ctx, s := buildmetric.Begin(context.Background())
		bad(ctx)
		if !s.Snapshot().Invalid || !s.Snapshot().Valid() {
			t.Fatal("invalid/overflow accounting was hidden")
		}
	}
}
