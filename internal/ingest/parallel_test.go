package ingest_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

type parallelCooldown struct {
	mu    sync.Mutex
	until time.Time
}

func (s *parallelCooldown) Cooldown(context.Context) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.until, nil
}
func (s *parallelCooldown) ExtendCooldown(_ context.Context, until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if until.After(s.until) {
		s.until = until
	}
	return nil
}
func parallelCoordinator(t *testing.T, transport http.RoundTripper, cooldown *parallelCooldown, attempts int) *ingest.Coordinator {
	t.Helper()
	client := sourceClient(t, transport)
	limiter, err := ingest.NewLimiter(3)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ingest.NewCoordinator(client, ingest.BudgetConfig{RequestTimeout: 3 * time.Second, PageTimeout: 4 * time.Second, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: attempts, RetryHTTP: []int{429, 503}}, ingest.SystemTimer{}, rand.Reader, cooldown, limiter)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParallelWindowsOverlapButCommitInOrderAndStayBounded(t *testing.T) {
	var mu sync.Mutex
	active, peak := 0, 0
	var requested, committed []int
	starts := [2]int{}
	gates := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	c := parallelCoordinator(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		mu.Lock()
		if page >= 5 && len(committed) < 4 {
			t.Error("unbounded prefetch crossed commit window")
		}
		requested = append(requested, page)
		active++
		peak = max(peak, active)
		if page > 1 {
			batch := (page - 2) / 3
			starts[batch]++
			if starts[batch] == 3 {
				close(gates[batch])
			}
		}
		mu.Unlock()
		defer func() { mu.Lock(); active--; mu.Unlock() }()
		if page > 1 {
			select {
			case <-gates[(page-2)/3]:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return oneRowResponse(7, fmt.Sprint(page)), nil
	}), &parallelCooldown{}, 1)
	r := pageRequest()
	r.PageSize = 1
	result, err := c.CollectConcurrent(t.Context(), r, taskBudget(t, time.Now().Add(5*time.Second), 20), collectLimits(), sinkFunc(func(_ context.Context, r ingest.PageRequest, _ ingest.Page, _ ingest.PageSummary) error {
		mu.Lock()
		defer mu.Unlock()
		committed = append(committed, r.Page)
		return nil
	}), 3)
	if err != nil || !result.Manifest.Complete || peak != 3 || active != 0 || len(requested) != 7 || !reflect.DeepEqual(committed, []int{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatal("parallel bounds/order", err, peak, requested, committed)
	}
}

func TestParallelFailureCancelsAndJoinsOtherPagesAndPreservesCooldown(t *testing.T) {
	for _, cooling := range []bool{false, true} {
		t.Run(fmt.Sprint(cooling), func(t *testing.T) {
			started := make(chan struct{})
			fourthStarted := make(chan struct{})
			stopped := make(chan struct{})
			store := &parallelCooldown{}
			c := parallelCoordinator(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if page == 1 {
					return oneRowResponse(8, "1"), nil
				}
				if page >= 5 {
					t.Error("requested a later window")
				}
				if page == 2 {
					close(started)
					<-r.Context().Done()
					close(stopped)
					return nil, r.Context().Err()
				}
				if page == 3 {
					select {
					case <-started:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
					select {
					case <-fourthStarted:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
					out := oneRowResponse(8, "3")
					out.StatusCode = 503
					if cooling {
						out.Header.Set("Retry-After", "60")
					}
					return out, nil
				}
				if page == 4 {
					close(fourthStarted)
				}
				return oneRowResponse(8, fmt.Sprint(page)), nil
			}), store, 1)
			r := pageRequest()
			r.PageSize = 1
			var committed []int
			result, err := c.CollectConcurrent(t.Context(), r, taskBudget(t, time.Now().Add(5*time.Second), 20), collectLimits(), sinkFunc(func(_ context.Context, r ingest.PageRequest, _ ingest.Page, _ ingest.PageSummary) error {
				committed = append(committed, r.Page)
				return nil
			}), 3)
			var failure *ingest.AcquisitionError
			if !errors.As(err, &failure) || failure.Page != 3 || result.Manifest.Complete || !reflect.DeepEqual(committed, []int{1}) {
				t.Fatal(err, committed)
			}
			select {
			case <-stopped:
			default:
				t.Fatal("returned with page still active")
			}
			until, _ := store.Cooldown(t.Context())
			if cooling && !until.After(time.Now()) {
				t.Fatal("cooldown lost")
			}
		})
	}
}

func TestParallelRetriesAndValidationKeepConfirmedPageProgress(t *testing.T) {
	for _, fault := range []string{"retry", "duplicate", "total", "commit", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			var mu sync.Mutex
			attempts := map[int]int{}
			c := parallelCoordinator(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				mu.Lock()
				attempts[page]++
				n := attempts[page]
				mu.Unlock()
				out := oneRowResponse(5, fmt.Sprint(page))
				if page == 2 && fault == "retry" && n == 1 {
					out.StatusCode = 503
				}
				if page == 3 && fault == "duplicate" {
					out = oneRowResponse(5, "2")
				}
				if page == 3 && fault == "total" {
					out = oneRowResponse(6, "3")
				}
				return out, nil
			}), &parallelCooldown{}, 2)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := pageRequest()
			r.PageSize = 1
			var committed []int
			result, err := c.CollectConcurrent(ctx, r, taskBudget(t, time.Now().Add(5*time.Second), 20), collectLimits(), sinkFunc(func(_ context.Context, r ingest.PageRequest, _ ingest.Page, _ ingest.PageSummary) error {
				if r.Page == 3 && fault == "commit" {
					return errors.New("private disk error")
				}
				committed = append(committed, r.Page)
				if r.Page == 2 && fault == "cancel" {
					cancel()
				}
				return nil
			}), 3)
			if fault == "retry" {
				if err != nil || !result.Manifest.Complete || attempts[2] != 2 || !reflect.DeepEqual(committed, []int{1, 2, 3, 4, 5}) {
					t.Fatal(err, attempts, committed)
				}
			} else if err == nil || result.Manifest.Complete || !reflect.DeepEqual(committed, []int{1, 2}) || len(result.Manifest.Pages) != 2 || attempts[5] != 0 {
				t.Fatal(err, attempts, committed)
			}
		})
	}
}
