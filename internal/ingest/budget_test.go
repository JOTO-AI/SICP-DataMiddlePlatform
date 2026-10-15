package ingest_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type budgetClock struct {
	now   time.Time
	waits []time.Duration
}

func (c *budgetClock) Now() time.Time { return c.now }
func (c *budgetClock) Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.waits = append(c.waits, d)
	c.now = c.now.Add(d)
	return nil
}

type savedCooldown struct {
	until               time.Time
	reads, writes       int
	failRead, failWrite bool
}

func (s *savedCooldown) Cooldown(context.Context) (time.Time, error) {
	s.reads++
	if s.failRead {
		return time.Time{}, errors.New("read failure")
	}
	return s.until, nil
}
func (s *savedCooldown) ExtendCooldown(ctx context.Context, until time.Time) error {
	s.writes++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.failWrite {
		return errors.New("write failure")
	}
	if until.After(s.until) {
		s.until = until
	}
	return nil
}

type admissionFunc func(context.Context) (func(), error)

func (a admissionFunc) Acquire(ctx context.Context) (func(), error) { return a(ctx) }
func configBudget() ingest.BudgetConfig {
	return ingest.BudgetConfig{RequestTimeout: 10 * time.Second, PageTimeout: 30 * time.Second, InitialBackoff: 2 * time.Second, MaxBackoff: 8 * time.Second, MaxAttempts: 3, RetryHTTP: []int{429, 503}}
}
func fakeBudgetClock() *budgetClock {
	return &budgetClock{now: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func taskBudget(t *testing.T, deadline time.Time, calls int) *ingest.TaskBudget {
	t.Helper()
	b, err := ingest.NewTaskBudget(deadline, calls)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func coordinator(t *testing.T, client *ingest.Client, cfg ingest.BudgetConfig, clock *budgetClock, store *savedCooldown, admission ingest.Admission) *ingest.Coordinator {
	t.Helper()
	c, err := ingest.NewCoordinator(client, cfg, clock, testkit.FixedRandom(0), store, admission)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func freeAdmission() ingest.Admission {
	return admissionFunc(func(context.Context) (func(), error) { return func() {}, nil })
}

func TestT03_16EarliestDeadlineIncludesQueueAndReading(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		request, page, task, want time.Duration
	}{
		{"request", 3 * time.Second, 20 * time.Second, 30 * time.Second, 3 * time.Second},
		{"page", 20 * time.Second, 4 * time.Second, 30 * time.Second, 4 * time.Second},
		{"task", 20 * time.Second, 30 * time.Second, 5 * time.Second, 5 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := fakeBudgetClock()
			start := clock.now
			calls, releases := 0, 0
			check := func(ctx context.Context) {
				d, ok := ctx.Deadline()
				if !ok || !d.Equal(start.Add(tc.want)) {
					t.Fatal("wrong effective deadline")
				}
			}
			client := sourceClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				check(r.Context())
				clock.now = start.Add(tc.want)
				return response(200, io.NopCloser(strings.NewReader(emptyResponse))), nil
			}))
			gate := admissionFunc(func(ctx context.Context) (func(), error) {
				check(ctx)
				clock.now = clock.now.Add(time.Second)
				return func() { releases++ }, nil
			})
			cfg := configBudget()
			cfg.RequestTimeout = tc.request
			cfg.PageTimeout = tc.page
			cfg.MaxAttempts = 1
			c := coordinator(t, client, cfg, clock, &savedCooldown{}, gate)
			r, err := c.FetchPage(context.Background(), pageRequest(), taskBudget(t, start.Add(tc.task), 9))
			if err == nil || r.Page.Records != nil || calls != 1 || releases != 1 {
				t.Fatal("late response accepted or permit leaked")
			}
		})
	}
}

func TestT03_16QueueExpiryPreventsSendAndBackoffConsumesPageBudget(t *testing.T) {
	clock := fakeBudgetClock()
	calls, releases := 0, 0
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, temporaryError{} }))
	gate := admissionFunc(func(context.Context) (func(), error) {
		clock.now = clock.now.Add(11 * time.Second)
		return func() { releases++ }, nil
	})
	c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, gate)
	task := taskBudget(t, clock.now.Add(time.Hour), 9)
	r, err := c.FetchPage(context.Background(), pageRequest(), task)
	if err == nil || calls != 0 || task.Calls() != 0 || releases != 1 || r.Attempts[0].Attempt.Sent {
		t.Fatal("expired queue sent request")
	}
	clock = fakeBudgetClock()
	cfg := configBudget()
	cfg.PageTimeout = 2500 * time.Millisecond
	c = coordinator(t, client, cfg, clock, &savedCooldown{}, freeAdmission())
	r, err = c.FetchPage(context.Background(), pageRequest(), taskBudget(t, clock.now.Add(time.Hour), 9))
	// First backoff is one second; second needs two seconds and exceeds the
	// remaining 1.5 seconds. No third network call is allowed.
	if err == nil || calls != 2 || len(clock.waits) != 1 || clock.waits[0] != time.Second || len(r.Attempts) != 2 {
		t.Fatal("backoff escaped page budget")
	}
}

func TestT03_16TaskCallLimitSharedAcrossPages(t *testing.T) {
	clock := fakeBudgetClock()
	calls := 0
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, io.NopCloser(strings.NewReader(emptyResponse))), nil
	}))
	c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
	task := taskBudget(t, clock.now.Add(time.Hour), 1)
	if _, err := c.FetchPage(context.Background(), pageRequest(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchPage(context.Background(), pageRequest(), task); err == nil || calls != 1 || task.Calls() != 1 {
		t.Fatal("per-page call budget reset")
	}
}

type lockedCooldown struct {
	mu sync.Mutex
	savedCooldown
}

func (s *lockedCooldown) Cooldown(ctx context.Context) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.savedCooldown.Cooldown(ctx)
}
func (s *lockedCooldown) ExtendCooldown(ctx context.Context, until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.savedCooldown.ExtendCooldown(ctx, until)
}

func TestT03_16TaskCallReservationsAreAtomic(t *testing.T) {
	clock := fakeBudgetClock()
	var calls atomic.Int32
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(200, io.NopCloser(strings.NewReader(emptyResponse))), nil
	}))
	limiter, _ := ingest.NewLimiter(8)
	c, err := ingest.NewCoordinator(client, configBudget(), clock, testkit.FixedRandom(0), &lockedCooldown{}, limiter)
	if err != nil {
		t.Fatal(err)
	}
	task := taskBudget(t, clock.now.Add(time.Hour), 16)
	var wg sync.WaitGroup
	var success atomic.Int32
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.FetchPage(context.Background(), pageRequest(), task); err == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 16 || success.Load() != 16 || task.Calls() != 16 {
		t.Fatal("concurrent calls exceeded shared budget")
	}
}

func TestT03_17SuccessfulResponseAlsoPreservesCooldown(t *testing.T) {
	clock := fakeBudgetClock()
	start := clock.now
	store := &savedCooldown{}
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		r := response(200, io.NopCloser(strings.NewReader(emptyResponse)))
		r.Header.Set("Retry-After", "3")
		return r, nil
	}))
	c := coordinator(t, client, configBudget(), clock, store, freeAdmission())
	if _, err := c.FetchPage(context.Background(), pageRequest(), taskBudget(t, start.Add(time.Hour), 9)); err != nil || !store.until.Equal(start.Add(3*time.Second)) {
		t.Fatal("successful response ignored cooldown")
	}
}

func TestPostResponseCooldownFailureRetainsActualRequest(t *testing.T) {
	clock := fakeBudgetClock()
	start := clock.now
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		r := response(200, io.NopCloser(strings.NewReader(emptyResponse)))
		r.Header.Set("Retry-After", "invalid-date")
		return r, nil
	}))
	c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
	_, err := c.FetchPage(context.Background(), pageRequest(), taskBudget(t, start.Add(time.Hour), 9))
	var failure *ingest.Failure
	if !errors.As(err, &failure) || len(failure.Attempt.Diagnostics) != 1 {
		t.Fatal("missing post-response failure")
	}
	r := failure.Attempt.Diagnostics[0]
	if !r.Sent || r.HTTPStatus != 200 || r.BusinessCode != "200" || r.Stage != "schedule" || r.StartedAt.IsZero() || r.Response.Unavailable != "success_payload_omitted" {
		t.Fatal("successful response invented as unsent request")
	}
}

func TestT03_17CooldownSurvivesCoordinatorAndTaskReplacement(t *testing.T) {
	for _, header := range []string{"60", "Fri, 01 Jan 2100 00:01:00 GMT"} {
		clock := fakeBudgetClock()
		start := clock.now
		store := &savedCooldown{}
		calls := 0
		client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			r := response(429, io.NopCloser(strings.NewReader("")))
			r.Header.Set("Retry-After", header)
			if calls > 1 {
				r = response(200, io.NopCloser(strings.NewReader(emptyResponse)))
			}
			return r, nil
		}))
		c := coordinator(t, client, configBudget(), clock, store, freeAdmission())
		r, err := c.FetchPage(context.Background(), pageRequest(), taskBudget(t, start.Add(20*time.Second), 9))
		if err == nil || calls != 1 || store.writes != 1 || !store.until.Equal(start.Add(time.Minute)) || r.Page.Records != nil {
			t.Fatal("cooldown not persisted")
		}
		c = coordinator(t, client, configBudget(), clock, store, freeAdmission())
		r, err = c.FetchPage(context.Background(), pageRequest(), taskBudget(t, start.Add(time.Hour), 9))
		if err == nil || calls != 1 || r.Attempts[0].Attempt.Sent {
			t.Fatal("new task bypassed cooldown")
		}
		clock.now = start.Add(time.Minute)
		if _, err = c.FetchPage(context.Background(), pageRequest(), taskBudget(t, start.Add(time.Hour), 9)); err != nil || calls != 2 {
			t.Fatal("expired cooldown blocked recovery")
		}
	}
}

func TestT03_17ShortCooldownWaitsAndFailedPersistenceStopsRetry(t *testing.T) {
	for _, fail := range []bool{false, true} {
		clock := fakeBudgetClock()
		start := clock.now
		store := &savedCooldown{failWrite: fail}
		calls := 0
		client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				r := response(429, io.NopCloser(strings.NewReader("")))
				r.Header.Set("Retry-After", "3")
				return r, nil
			}
			if clock.now.Before(start.Add(3 * time.Second)) {
				t.Fatal("cooldown shortened")
			}
			return response(200, io.NopCloser(strings.NewReader(emptyResponse))), nil
		}))
		c := coordinator(t, client, configBudget(), clock, store, freeAdmission())
		_, err := c.FetchPage(context.Background(), pageRequest(), taskBudget(t, start.Add(time.Hour), 9))
		if fail {
			if err == nil || calls != 1 {
				t.Fatal("failed persistence continued")
			}
		} else if err != nil || calls != 2 {
			t.Fatalf("short cooldown did not recover: %v", err)
		}
	}
}

func TestT03_17LateResponseStillPersistsCooldown(t *testing.T) {
	clock := fakeBudgetClock()
	start := clock.now
	store := &savedCooldown{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		clock.now = start.Add(40 * time.Second)
		cancel()
		r := response(429, io.NopCloser(strings.NewReader("")))
		r.Header.Set("Retry-After", "60")
		return r, nil
	}))
	c := coordinator(t, client, configBudget(), clock, store, freeAdmission())
	if _, err := c.FetchPage(ctx, pageRequest(), taskBudget(t, start.Add(20*time.Second), 9)); err == nil || store.writes != 1 || !store.until.Equal(start.Add(100*time.Second)) {
		t.Fatal("late response lost cooldown")
	}
}

func TestT03_17InvalidCooldownAndStoreReadFailureFailClosed(t *testing.T) {
	for _, header := range []string{"invalid", "18446744073709551615", "-1"} {
		clock := fakeBudgetClock()
		calls := 0
		store := &savedCooldown{}
		client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			r := response(429, io.NopCloser(strings.NewReader("")))
			r.Header.Set("Retry-After", header)
			return r, nil
		}))
		c := coordinator(t, client, configBudget(), clock, store, freeAdmission())
		if _, err := c.FetchPage(context.Background(), pageRequest(), taskBudget(t, clock.now.Add(time.Hour), 9)); err == nil || calls != 1 || store.writes != 0 {
			t.Fatal("invalid cooldown retried")
		}
		store.failRead = true
		if _, err := c.FetchPage(context.Background(), pageRequest(), taskBudget(t, clock.now.Add(time.Hour), 9)); err == nil || calls != 1 {
			t.Fatal("missing durable cooldown allowed send")
		}
	}
}

func TestT03_16LimiterCancellationAndReleaseAreBounded(t *testing.T) {
	limiter, err := ingest.NewLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	release, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = limiter.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled admission")
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); release() }()
	}
	wg.Wait()
	release, err = limiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}
