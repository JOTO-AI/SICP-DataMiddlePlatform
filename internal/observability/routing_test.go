package observability_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type sourcePipeline struct {
	journal  *journal
	quota    *observability.QuotaJournal
	buffer   *observability.Buffer
	store    *logStore
	dispatch *observability.Dispatcher
}

func TestT28_05RepositoryUsesExplicitSourceWithoutFallback(t *testing.T) {
	r := logRegistry(t, true)
	ctx := context.Background()
	for _, source := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		db := &logsDB{}
		repo := observability.Repository{Registry: r, Database: db, Source: source}
		kind, wrong := events.Request, events.Call
		if source == storage.UpstreamLog {
			kind, wrong = wrong, kind
		}
		if err := repo.StoreBatch(ctx, []observability.SafeEvent{sourceEvent(t, kind, "good")}); err != nil {
			t.Fatal(err)
		}
		target, _ := r.LogConnection(source)
		if len(db.connections) != 1 || db.connections[0] != target.DSN() {
			t.Fatal("wrong database")
		}
		if err := repo.StoreBatch(ctx, []observability.SafeEvent{sourceEvent(t, wrong, "bad")}); err != observability.ErrPayload || len(db.connections) != 1 {
			t.Fatal("wrong source reached SQL")
		}
	}
	db := &logsDB{}
	repo := observability.Repository{Registry: r, Database: db}
	if err := repo.StoreBatch(ctx, []observability.SafeEvent{sourceEvent(t, events.Request, "bad")}); err != observability.ErrDelivery || len(db.connections) != 0 {
		t.Fatal("implicit dual fallback")
	}
}

func TestT28_08RejectsSharedJournalAndApplicationQuotaOverflow(t *testing.T) {
	j := newJournal()
	s := sanitizer(t)
	p, _ := observability.NewSourceBuffer(storage.PlatformLog, quota(t, j), s, 10, 32768)
	u, _ := observability.NewSourceBuffer(storage.UpstreamLog, quota(t, j), s, 10, 32768)
	if _, err := observability.NewRouter(p, u); err == nil {
		t.Fatal("shared journal accepted")
	}
	q, err := observability.NewQuota(j, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 2, WarningPercent: 80, Policy: observability.RejectNew})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := observability.NewSourceBuffer(storage.PlatformLog, q, s, 10, 32768)
	if b.Append(t.Context(), sourceEvent(t, events.Request, "first")) != nil {
		t.Fatal("first append")
	}
	if b.Append(t.Context(), sourceEvent(t, events.Request, "second")) != observability.ErrCapacity || q.Status().Rejected != 1 || len(j.files) != 1 {
		t.Fatal("quota overflow lost original")
	}
}

func TestT28_09BlockedDeliveryCloseHonorsBudget(t *testing.T) {
	ctx := context.Background()
	clock := &dispatchClock{now: event().OccurredAt.Add(time.Minute)}
	p := pipeline(t, storage.UpstreamLog, clock)
	if p.buffer.Append(ctx, sourceEvent(t, events.Call, "blocked")) != nil {
		t.Fatal("append")
	}
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	d, err := observability.NewDispatcher(p.buffer, logStoreFunc(func(context.Context, []observability.SafeEvent) error { close(entered); <-release; return nil }), clock, observability.DispatchConfig{MaxWait: time.Second, RetryWait: time.Second, Timeout: time.Second, BatchBytes: 32768})
	if err != nil {
		t.Fatal(err)
	}
	go func() { defer close(returned); _, _ = d.Pump(ctx) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("delivery not entered")
	}
	budget, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if d.Close(budget) != observability.ErrIncomplete || len(p.journal.files) != 1 {
		t.Fatal("blocked delivery falsely drained")
	}
	close(release)
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("delivery leaked")
	}
}

func pipeline(t *testing.T, source storage.LogSource, clock *dispatchClock) sourcePipeline {
	t.Helper()
	j := newJournal()
	q := quota(t, j)
	b, err := observability.NewSourceBuffer(source, q, sanitizer(t), 10, 32768)
	if err != nil {
		t.Fatal(err)
	}
	s := &logStore{}
	return sourcePipeline{j, q, b, s, dispatcher(t, b, s, clock)}
}

func sourceEvent(t *testing.T, kind events.Kind, id string) observability.SafeEvent {
	t.Helper()
	e := event()
	e.ID = id
	e.Kind = kind
	if kind == events.Audit {
		e.Phase, e.Outcome = events.Finished, events.Succeeded
		e.Audit = &events.AuditIdentity{ActorID: "actor-private", Action: "source_test", ObjectID: "object-private"}
	}
	if kind == events.Call || kind == events.Attempt {
		e.Identity.Purpose, e.Identity.TestRunID = events.TestPurpose, "test-private"
	}
	s, err := sanitizer(t).Sanitize(e, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestT28_05RoutesOnlySanitizedKnownKinds(t *testing.T) {
	ctx := context.Background()
	clock := &dispatchClock{now: event().OccurredAt}
	p, u := pipeline(t, storage.PlatformLog, clock), pipeline(t, storage.UpstreamLog, clock)
	r, err := observability.NewRouter(p.buffer, u.buffer)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []events.Kind{events.Request, events.Task, events.Step, events.Audit, events.Call, events.Attempt} {
		s := sourceEvent(t, k, string(k))
		if err := r.Append(ctx, s); err != nil {
			t.Fatal(err)
		}
		for _, raw := range []string{"actor-private", "object-private", "test-private"} {
			if strings.Contains(string(s.Bytes()), raw) {
				t.Fatal("raw identity persisted")
			}
		}
	}
	if len(p.journal.files) != 4 || len(u.journal.files) != 2 {
		t.Fatal("incorrect routing")
	}
	if p.buffer.Append(ctx, sourceEvent(t, events.Call, "wrong")) != observability.ErrPayload {
		t.Fatal("wrong source accepted")
	}
	if u.buffer.Append(ctx, sourceEvent(t, events.Request, "wrong")) != observability.ErrPayload {
		t.Fatal("wrong source accepted")
	}
	if r.Append(ctx, observability.SafeEvent{}) != observability.ErrPayload {
		t.Fatal("zero event accepted")
	}
	e := event()
	e.Kind = "unknown"
	if _, err := sanitizer(t).Sanitize(e, observability.Details{}); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestT28_06EachFailedSourceRetainsWhileOtherDelivers(t *testing.T) {
	for _, down := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		t.Run(string(down), func(t *testing.T) {
			ctx := context.Background()
			clock := &dispatchClock{now: event().OccurredAt.Add(time.Minute)}
			p, u := pipeline(t, storage.PlatformLog, clock), pipeline(t, storage.UpstreamLog, clock)
			r, _ := observability.NewRouter(p.buffer, u.buffer)
			if r.Append(ctx, sourceEvent(t, events.Request, "p")) != nil || r.Append(ctx, sourceEvent(t, events.Call, "u")) != nil {
				t.Fatal("append")
			}
			bad, good := p, u
			if down == storage.UpstreamLog {
				bad, good = u, p
			}
			bad.store.down = true
			if _, err := bad.dispatch.Pump(ctx); err == nil {
				t.Fatal("failure hidden")
			}
			if _, err := good.dispatch.Pump(ctx); err != nil {
				t.Fatal(err)
			}
			if len(bad.journal.files) != 1 || len(good.journal.files) != 0 || good.dispatch.Stats().Delivered != 1 || bad.dispatch.Stats().Failures != 1 {
				t.Fatal("sources coupled")
			}
		})
	}
}

func TestT28_07LostReplyAndConflictKeepOriginalPayload(t *testing.T) {
	ctx := context.Background()
	clock := &dispatchClock{now: event().OccurredAt.Add(time.Minute)}
	for _, src := range []storage.LogSource{storage.PlatformLog, storage.UpstreamLog} {
		p := pipeline(t, src, clock)
		kind := events.Request
		if src == storage.UpstreamLog {
			kind = events.Call
		}
		s := sourceEvent(t, kind, "same")
		if p.buffer.Append(ctx, s) != nil {
			t.Fatal("append")
		}
		p.store.lostReply = true
		if _, err := p.buffer.Deliver(ctx, p.store); !errors.Is(err, observability.ErrDelivery) || len(p.journal.files) != 1 {
			t.Fatal("unknown commit reclaimed")
		}
		rebuilt, err := observability.NewSourceBuffer(src, quota(t, p.journal), sanitizer(t), 10, 32768)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := rebuilt.Deliver(ctx, p.store); err != nil || n != 1 || len(p.store.rows) != 1 {
			t.Fatal("replay duplicated")
		}
		if rebuilt.Append(ctx, s) != nil {
			t.Fatal("append")
		}
		p.store.rows[s.ID()] = []byte(`{"different":true}`)
		if _, err := rebuilt.Deliver(ctx, p.store); err == nil || len(p.journal.files) != 1 {
			t.Fatal("conflict reclaimed")
		}
	}
}

func TestT28_08QuotaAndLowTrafficAreIndependent(t *testing.T) {
	ctx := context.Background()
	clock := &dispatchClock{now: event().OccurredAt}
	p, u := pipeline(t, storage.PlatformLog, clock), pipeline(t, storage.UpstreamLog, clock)
	r, _ := observability.NewRouter(p.buffer, u.buffer)
	if r.Append(ctx, sourceEvent(t, events.Call, "old")) != nil {
		t.Fatal("append")
	}
	u.journal.failPut = true
	if err := r.Append(ctx, sourceEvent(t, events.Call, "new")); !errors.Is(err, observability.ErrCapacity) {
		t.Fatal("capacity hidden")
	}
	if r.Append(ctx, sourceEvent(t, events.Request, "healthy")) != nil {
		t.Fatal("unrelated source blocked")
	}
	if u.quota.Status().Rejected != 1 || p.quota.Status().Rejected != 0 || len(u.journal.files) != 1 {
		t.Fatal("quota state shared")
	}
	for _, d := range []*observability.Dispatcher{p.dispatch, u.dispatch} {
		if _, err := d.Pump(ctx); err != nil {
			t.Fatal(err)
		}
	}
	clock.now = clock.now.Add(10 * time.Second)
	for _, d := range []*observability.Dispatcher{p.dispatch, u.dispatch} {
		if _, err := d.Pump(ctx); err != nil || d.Stats().Delivered != 1 {
			t.Fatal("low traffic not delivered")
		}
	}
}

func TestT28_09CloseRetainsUnconfirmedAndRebuildReplays(t *testing.T) {
	ctx := context.Background()
	clock := &dispatchClock{now: event().OccurredAt}
	p := pipeline(t, storage.UpstreamLog, clock)
	s := sourceEvent(t, events.Call, "pending")
	if p.buffer.Append(ctx, s) != nil {
		t.Fatal("append")
	}
	p.store.down = true
	budget, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if p.dispatch.Close(budget) != observability.ErrIncomplete || len(p.journal.files) != 1 {
		t.Fatal("false clean shutdown")
	}
	if p.buffer.Append(ctx, s) != observability.ErrClosed {
		t.Fatal("closed source accepted event")
	}
	b, err := observability.NewSourceBuffer(storage.UpstreamLog, quota(t, p.journal), sanitizer(t), 10, 32768)
	if err != nil {
		t.Fatal(err)
	}
	p.store.down = false
	if n, err := b.Deliver(ctx, p.store); err != nil || n != 1 || len(p.journal.files) != 0 {
		t.Fatal("rebuild lost pending event")
	}
	// A mixed legacy directory cannot silently move old calls to a new target.
	legacy := newJournal()
	if legacy.Put(ctx, s.ID(), s.Bytes()) != nil {
		t.Fatal("seed")
	}
	wrong, _ := observability.NewSourceBuffer(storage.PlatformLog, quota(t, legacy), sanitizer(t), 10, 32768)
	if _, err := wrong.Pending(ctx); err != observability.ErrPayload {
		t.Fatal("legacy event reclassified")
	}
}
