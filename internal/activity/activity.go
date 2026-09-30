// Package activity reports display-only task stages. It never authorizes
// publication, recovery or source-watermark advancement.
package activity

import (
	"context"
	"slices"
	"sync"
	"time"
)

type Snapshot struct {
	Phase       string    `json:"phase"`
	Table       string    `json:"table,omitempty"`
	Tables      []string  `json:"tables,omitempty"`
	Instance    string    `json:"instance,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
}

type observerKey struct{}
type tablesKey struct{}

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
	mu      sync.Mutex
	ctx     context.Context
	now     func() time.Time
	save    func(context.Context, Snapshot) error
	observe func(error)
	latest  Snapshot
}

func (t *tracker) report(phase, table, instance string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctx.Err() != nil {
		return
	}
	if t.latest.Phase != phase || t.latest.Table != table || t.latest.Instance != instance || len(t.latest.Tables) > 0 {
		t.latest = Snapshot{Phase: phase, Table: table, Instance: instance, StartedAt: t.now()}
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
		t.latest = Snapshot{Phase: phase, StartedAt: t.now()}
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
	return context.WithValue(ctx, tablesKey{}, t.reportTables), func() { cancel(); <-done }
}
