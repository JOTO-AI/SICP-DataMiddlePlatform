// Package activity reports display-only task stages. It never authorizes
// publication, recovery or source-watermark advancement.
package activity

import (
	"context"
	"math"
	"slices"
	"sync"
	"time"
)

type Snapshot struct {
	Phase               string     `json:"phase"`
	Table               string     `json:"table,omitempty"`
	Tables              []string   `json:"tables,omitempty"`
	Instance            string     `json:"instance,omitempty"`
	StartedAt           time.Time  `json:"started_at"`
	HeartbeatAt         time.Time  `json:"heartbeat_at"`
	Unit                string     `json:"unit,omitempty"`
	Detail              string     `json:"detail,omitempty"`
	Completed           int64      `json:"completed,omitempty"`
	Total               int64      `json:"total,omitempty"`
	TotalKnown          bool       `json:"total_known,omitempty"`
	Batches             int64      `json:"batches,omitempty"`
	Attempt             int64      `json:"attempt,omitempty"`
	CumulativeCompleted int64      `json:"cumulative_completed,omitempty"`
	CumulativeElapsedMS int64      `json:"cumulative_elapsed_ms,omitempty"`
	LastProgressAt      *time.Time `json:"last_progress_at,omitempty"`
}

type observerKey struct{}
type tablesKey struct{}
type workKey struct{}
type advanceKey struct{}

func Work(ctx context.Context, phase, table, instance, unit, detail string, total int64) context.Context {
	return context.WithValue(ctx, advanceKey{}, Begin(ctx, phase, table, instance, unit, detail, total))
}

func Advance(ctx context.Context, completed, batches int64) {
	if advance, ok := ctx.Value(advanceKey{}).(func(int64, int64)); ok {
		advance(completed, batches)
	}
}

// Begin starts a display scope. Its callback only updates bounded in-memory
// counters; the existing heartbeat persists them. Late callbacks are ignored.
// A negative total means unknown. Completion never constitutes ready evidence.
func Begin(ctx context.Context, phase, table, instance, unit, detail string, total int64) func(int64, int64) {
	if t, ok := ctx.Value(workKey{}).(*tracker); ok {
		return t.begin(phase, table, instance, unit, detail, total)
	}
	return func(int64, int64) {}
}

type history struct{ attempts, completed, elapsedMS int64 }

func ReportTables(ctx context.Context, phase string, tables []string) {
	if report, ok := ctx.Value(tablesKey{}).(func(string, []string)); ok {
		report(phase, tables)
	}
}

func Report(ctx context.Context, phase, table, instance string) {
	if report, ok := ctx.Value(observerKey{}).(func(string, string, string)); ok {
		report(phase, table, instance)
	}
}

type tracker struct {
	mu       sync.Mutex
	ctx      context.Context
	now      func() time.Time
	save     func(context.Context, Snapshot) error
	observe  func(error)
	latest   Snapshot
	sequence uint64
	history  map[[5]string]history
	key      [5]string
	prior    history
}

func (t *tracker) reset(phase, table, instance, unit, detail string, total int64) {
	if t.history == nil {
		t.history = make(map[[5]string]history)
	}
	if t.latest.Phase != "" {
		t.history[t.key] = history{t.latest.Attempt, t.latest.CumulativeCompleted, t.prior.elapsedMS + max(0, t.now().Sub(t.latest.StartedAt).Milliseconds())}
	}
	t.key = [5]string{phase, table, instance, unit, detail}
	t.prior = t.history[t.key]
	t.sequence++
	t.latest = Snapshot{Phase: phase, Table: table, Instance: instance, Unit: unit, Detail: detail,
		StartedAt: t.now(), Attempt: t.prior.attempts + 1, CumulativeCompleted: t.prior.completed,
		TotalKnown: total >= 0, Total: max(0, total)}
}

func (t *tracker) begin(phase, table, instance, unit, detail string, total int64) func(int64, int64) {
	t.mu.Lock()
	if t.ctx.Err() != nil {
		t.mu.Unlock()
		return func(int64, int64) {}
	}
	t.reset(phase, table, instance, unit, detail, total)
	seq := t.sequence
	t.persist()
	t.mu.Unlock()
	return func(completed, batches int64) {
		t.mu.Lock()
		defer t.mu.Unlock()
		p := &t.latest
		if t.ctx.Err() != nil || t.sequence != seq || completed < 0 || batches < 0 ||
			completed > math.MaxInt64-p.Completed || completed > math.MaxInt64-p.CumulativeCompleted || batches > math.MaxInt64-p.Batches {
			return
		}
		if completed == 0 && batches == 0 {
			return
		}
		p.Completed += completed
		p.CumulativeCompleted += completed
		p.Batches += batches
		now := t.now()
		p.LastProgressAt = &now
	}
}

func (t *tracker) report(phase, table, instance string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctx.Err() != nil {
		return
	}
	if t.latest.Phase != phase || t.latest.Table != table || t.latest.Instance != instance || len(t.latest.Tables) > 0 {
		t.reset(phase, table, instance, "", "", -1)
	}
	t.persist()
}

func (t *tracker) reportTables(phase string, tables []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctx.Err() != nil {
		return
	}
	if t.latest.Phase != phase || t.latest.Table != "" || t.latest.Instance != "" {
		t.reset(phase, "", "", "", "", -1)
	}
	t.latest.Tables = slices.Clone(tables)
	t.persist()
}

func (t *tracker) pulse() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctx.Err() == nil && t.latest.Phase != "" {
		t.persist()
	}
}

func (t *tracker) persist() {
	t.latest.HeartbeatAt = t.now()
	t.latest.CumulativeElapsedMS = t.prior.elapsedMS + max(0, t.latest.HeartbeatAt.Sub(t.latest.StartedAt).Milliseconds())
	ctx, cancel := context.WithTimeout(t.ctx, 5*time.Second)
	defer cancel()
	if err := t.save(ctx, t.latest); err != nil && t.observe != nil {
		t.observe(err)
	}
}

// Track emits a heartbeat even while a single validation or build call runs
// for minutes. A heartbeat means the executor is alive, not that rows advanced.
// stop cancels and joins telemetry so completed tasks cannot keep heartbeating.
func Track(ctx context.Context, now func() time.Time, save func(context.Context, Snapshot) error, observe func(error)) (context.Context, func()) {
	live, cancel := context.WithCancel(ctx)
	t := &tracker{ctx: live, now: now, save: save, observe: observe}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-live.Done():
				return
			case <-ticker.C:
				t.pulse()
			}
		}
	}()
	ctx = context.WithValue(ctx, observerKey{}, t.report)
	ctx = context.WithValue(ctx, workKey{}, t)
	return context.WithValue(ctx, tablesKey{}, t.reportTables), func() { cancel(); <-done }
}
