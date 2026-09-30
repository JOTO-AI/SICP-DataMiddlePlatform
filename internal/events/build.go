package events

import (
	"context"
	"errors"
	"math"
	"sync"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// BuildScope identifies measured work, never evidence authorizing a transition.
// The task and physical instance live in Event.Identity. Overlapping scopes
// must not be summed to obtain end-to-end elapsed time.
type BuildScope struct {
	Stage       string             `json:"stage"`
	Table       string             `json:"table,omitempty"`
	Combination string             `json:"combination,omitempty"`
	Candidate   string             `json:"candidate,omitempty"`
	Slot        int                `json:"slot,omitempty"`
	Generation  int64              `json:"generation,omitempty"`
	Operation   contract.Operation `json:"operation,omitempty"`
}

type BuildStage struct {
	BuildScope
	SessionID       string `json:"session_id"`
	OwnerGeneration int64  `json:"owner_generation"`
	Attempt         int    `json:"attempt"`
	CumulativeNS    int64  `json:"cumulative_ns"`
}

func (s BuildStage) valid() bool {
	if s.SessionID == "" || !validID(s.SessionID) || s.OwnerGeneration < 1 || s.Attempt < 1 || s.CumulativeNS < 0 || !validID(s.Combination) || !validID(s.Candidate) || s.Generation < 0 || s.Slot < 0 || s.Slot > 1 {
		return false
	}
	switch s.Stage {
	case "sealing", "prepare_target", "merge_table", "write_copy", "validate_copy", "content_check", "content_table", "index_check", "business_check", "old_copy_check", "route_shift", "drain", "publish", "publish_confirm", "confirm_progress":
	default:
		return false
	}
	if s.Table != "" {
		found := false
		for _, table := range source.Catalog() {
			found = found || table.Name == s.Table
		}
		if !found {
			return false
		}
	}
	if s.Operation != "" {
		found := false
		for _, op := range contract.Definitions() {
			found = found || op.Operation == s.Operation
		}
		if !found {
			return false
		}
	}
	return true
}

type buildKey struct{}
type buildScopeKey struct {
	BuildScope
	Instance string
}
type buildTotals struct {
	attempt int
	elapsed int64
}
type buildTrace struct {
	mu      sync.Mutex
	binding binding
	owner   int64
	session string
	observe func(error)
	totals  map[buildScopeKey]buildTotals
}

// WithBuildTrace binds one execution ownership generation. Retries in this
// execution retain cumulative time; recovered executions get a fresh session
// and are distinguishable by session_id even when the same process retries
// without changing the ownership generation.
func WithBuildTrace(ctx context.Context, ownerGeneration int64, observe func(error)) context.Context {
	b, _ := ctx.Value(contextKey{}).(binding)
	t := &buildTrace{binding: b, owner: ownerGeneration, observe: observe, totals: map[buildScopeKey]buildTotals{}}
	if b.recorder != nil {
		var err error
		t.session, err = b.recorder.newID()
		if err != nil {
			t.report(err)
			t.binding.recorder = nil
		}
	}
	return context.WithValue(ctx, buildKey{}, t)
}

func (t *buildTrace) report(err error) {
	if err != nil && t.observe != nil {
		// Never forward storage errors, SQL, source values or credentials.
		t.observe(ErrRecord)
	}
}

func (t *buildTrace) record(ctx context.Context, e Event) bool {
	if err := e.Validate(); err != nil {
		t.report(err)
		return false
	}
	r := t.binding.recorder
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.budget)
	defer cancel()
	err := r.sink.Record(recordCtx, e)
	if err == nil {
		err = recordCtx.Err()
	}
	t.report(err)
	return err == nil
}

// BeginBuild records actual call boundaries without changing business outcomes.
// The returned end function is idempotent. No goroutine or database lock is
// added: the existing Sink persists locally and honors its bounded context.
// A missing start acknowledgement suppresses the terminal event, so telemetry
// loss never fabricates a complete timing sample.
func BeginBuild(ctx context.Context, scope BuildScope, instance string) func(error) {
	t, _ := ctx.Value(buildKey{}).(*buildTrace)
	if t == nil || t.binding.recorder == nil {
		return func(error) {}
	}
	meta := BuildStage{BuildScope: scope, SessionID: t.session, OwnerGeneration: t.owner, Attempt: 1}
	identity := Identity{TaskID: t.binding.identity.TaskID, Instance: instance}
	if !meta.valid() || identity.TaskID == "" || !identity.valid() {
		t.report(ErrEvent)
		return func(error) {}
	}
	r := t.binding.recorder
	key := buildScopeKey{scope, instance}
	t.mu.Lock()
	previous := t.totals[key]
	if len(t.totals) >= 1024 && previous.attempt == 0 || previous.attempt == math.MaxInt {
		t.mu.Unlock()
		t.report(ErrEvent)
		return func(error) {}
	}
	previous.attempt++
	t.totals[key] = previous
	t.mu.Unlock()
	meta.Attempt, meta.CumulativeNS = previous.attempt, previous.elapsed
	id, err := r.newID()
	if err != nil {
		t.report(err)
		return func(error) {}
	}
	identity.StepID = id
	started := r.clock.Now() // Preserve the monotonic component for Sub below.
	e := Event{ID: id + "_start", EntityID: id, Kind: Step, Phase: Started, Identity: identity, OccurredAt: started.UTC(), Metadata: Metadata{Build: &meta}}
	if !t.record(ctx, e) {
		return func(error) {}
	}
	var once sync.Once
	return func(workErr error) {
		once.Do(func() {
			now := r.clock.Now()
			elapsed := now.Sub(started).Nanoseconds()
			t.mu.Lock()
			total := t.totals[key]
			if elapsed < 0 || elapsed > math.MaxInt64-total.elapsed {
				t.mu.Unlock()
				t.report(ErrEvent)
				return
			}
			total.elapsed += elapsed
			t.totals[key] = total
			t.mu.Unlock()
			endMeta := meta
			endMeta.CumulativeNS = total.elapsed
			end := e
			end.ID, end.Phase, end.OccurredAt = id+"_finished", Finished, now.UTC()
			end.Metadata = Metadata{Build: &endMeta}
			end.DurationNS = &elapsed
			end.Outcome = Succeeded
			if ctx.Err() != nil || errors.Is(workErr, context.Canceled) || errors.Is(workErr, context.DeadlineExceeded) {
				end.Outcome = Interrupted
			} else if workErr != nil {
				end.Outcome = Failed
			}
			t.record(ctx, end)
		})
	}
}
