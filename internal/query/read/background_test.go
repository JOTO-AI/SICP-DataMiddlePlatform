package read_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestBT02_01StreamingExceedsFrontendWithoutRetainingTraffic(t *testing.T) {
	const total = 100001
	r := matcherFunc(func(_ context.Context, _ read.Handle, _, after string, limit int, _ []source.Filter) ([]source.SourceRow, error) {
		start, _ := strconv.Atoi(after)
		rows := make([]source.SourceRow, 0, limit)
		for i := start + 1; i <= total && len(rows) < limit; i++ {
			rows = append(rows, sourceGrade(fmt.Sprintf("%09d", i)))
		}
		return rows, nil
	})
	for _, background := range []bool{false, true} {
		l := read.DefaultLimits()
		if background {
			l, _ = read.TableLimits(total, time.Minute)
			l.Bytes = 1
		}
		b, err := read.NewBudget(l, &scanClock{now: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		err = read.Scan(t.Context(), r, original, "dwd_bzkscjzlb", nil, b, func(source.SourceRow) error { count++; return nil })
		if background && (err != nil || count != total) {
			t.Fatal(count, err)
		}
		if !background && !errors.Is(err, read.ErrBudget) {
			t.Fatal("frontend widened", err)
		}
	}
}

func TestBT02_03DeclarationsAndRetainedPayloadAreBounded(t *testing.T) {
	for _, n := range []int64{-1, read.MaxBackgroundTableRows + 1, math.MaxInt64} {
		if _, err := read.TableLimits(n, time.Minute); err == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
	l := read.BackgroundLimits(time.Minute)
	l.Bytes = 5
	r, b, _, _ := scanFixture(t, []source.SourceRow{sourceGrade("01")}, l)
	if rows, err := read.Collect(t.Context(), r, original, "dwd_bzkscjzlb", nil, b); rows != nil || !errors.Is(err, read.ErrBudget) {
		t.Fatal("unbounded retained payload", err)
	}
	l.Bytes = 100
	l.BatchBytes = 5
	r, b, _, _ = scanFixture(t, []source.SourceRow{sourceGrade("01")}, l)
	if err := read.Scan(t.Context(), r, original, "dwd_bzkscjzlb", nil, b, func(source.SourceRow) error { return nil }); !errors.Is(err, read.ErrBudget) {
		t.Fatal("unbounded batch", err)
	}
}

func TestBT02_07BackgroundClockStillHasDeadline(t *testing.T) {
	l := read.BackgroundLimits(time.Minute)
	r, b, clock, _ := scanFixture(t, []source.SourceRow{sourceGrade("01")}, l)
	clock.now = clock.now.Add(21 * time.Second)
	if err := read.Scan(t.Context(), r, original, "dwd_bzkscjzlb", nil, b, func(source.SourceRow) error { return nil }); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Minute)
	if err := b.Check(t.Context()); !errors.Is(err, read.ErrBudget) {
		t.Fatal("background deadline ignored", err)
	}
}
