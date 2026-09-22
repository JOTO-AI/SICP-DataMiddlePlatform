package ingest_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

type sinkFunc func(context.Context, ingest.PageRequest, ingest.Page, ingest.PageSummary) error

func (s sinkFunc) CommitPage(ctx context.Context, r ingest.PageRequest, p ingest.Page, m ingest.PageSummary) error {
	return s(ctx, r, p, m)
}
func collectLimits() ingest.PageLimits {
	return ingest.PageLimits{MaxPages: 10, MaxRecords: 100, MaxBytes: 1 << 20}
}
func oneRowResponse(total int, key string) *http.Response {
	return response(200, io.NopCloser(strings.NewReader(fmt.Sprintf(`{"code":200,"message":"ok","data":{"total":%d,"list":[{"XQH":%q}]}}`, total, key))))
}

func TestT03_26MiddlePageExhaustionStopsLaterPagesAndLeavesPartialManifest(t *testing.T) {
	clock := fakeBudgetClock()
	var requested, committed []int
	client := sourceClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		requested = append(requested, page)
		if page == 2 {
			return nil, temporaryError{}
		}
		return oneRowResponse(3, "01"), nil
	}))
	c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
	r := pageRequest()
	r.PageSize = 1
	result, err := c.Collect(context.Background(), r, taskBudget(t, clock.now.Add(time.Hour), 10), collectLimits(), sinkFunc(func(_ context.Context, r ingest.PageRequest, _ ingest.Page, _ ingest.PageSummary) error {
		committed = append(committed, r.Page)
		return nil
	}))
	var failure *ingest.AcquisitionError
	if !errors.As(err, &failure) || failure.Page != 2 || !reflect.DeepEqual(requested, []int{1, 2, 2, 2}) || !reflect.DeepEqual(committed, []int{1}) || result.Manifest.Complete || result.Manifest.Rows != 1 || len(result.Manifest.Pages) != 1 || len(result.Calls) != 2 || len(result.Calls[1].Attempts) != 3 {
		t.Fatalf("bad final failure: %v", err)
	}
}

func TestT03_26RecoveredPageCommitsOnceAndCompletesWithoutTail(t *testing.T) {
	clock := fakeBudgetClock()
	var requested, committed []int
	secondAttempts := 0
	client := sourceClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		requested = append(requested, page)
		if page == 2 {
			secondAttempts++
			if secondAttempts == 1 {
				return nil, temporaryError{}
			}
		}
		return oneRowResponse(3, fmt.Sprintf("%02d", page)), nil
	}))
	c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
	r := pageRequest()
	r.PageSize = 1
	result, err := c.Collect(context.Background(), r, taskBudget(t, clock.now.Add(time.Hour), 10), collectLimits(), sinkFunc(func(_ context.Context, r ingest.PageRequest, p ingest.Page, m ingest.PageSummary) error {
		if m.Number != r.Page || m.Count != len(p.Records) {
			t.Fatal("page evidence mismatch")
		}
		committed = append(committed, r.Page)
		return nil
	}))
	if err != nil || !reflect.DeepEqual(requested, []int{1, 2, 2, 3}) || !reflect.DeepEqual(committed, []int{1, 2, 3}) || !result.Manifest.Complete || result.Manifest.Rows != 3 || result.FinalFailure != nil {
		t.Fatalf("bad collection: %v", err)
	}
}

func TestT03_26CancellationAndTaskBudgetStopBeforeNextPage(t *testing.T) {
	for _, mode := range []string{"before", "after_commit", "deadline"} {
		clock := fakeBudgetClock()
		calls, commits := 0, 0
		ctx, cancel := context.WithCancel(context.Background())
		if mode == "before" {
			cancel()
		}
		client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) { calls++; return oneRowResponse(2, "01"), nil }))
		c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
		r := pageRequest()
		r.PageSize = 1
		result, err := c.Collect(ctx, r, taskBudget(t, clock.now.Add(10*time.Second), 10), collectLimits(), sinkFunc(func(context.Context, ingest.PageRequest, ingest.Page, ingest.PageSummary) error {
			commits++
			if mode == "deadline" {
				clock.now = clock.now.Add(11 * time.Second)
			} else {
				cancel()
			}
			return nil
		}))
		cancel()
		if err == nil || result.Manifest.Complete {
			t.Fatal("cancelled acquisition completed")
		}
		if mode == "before" {
			if calls != 0 || commits != 0 || !errors.Is(err, context.Canceled) {
				t.Fatal("pre-cancel sent request")
			}
		} else if calls != 1 || commits != 1 || result.Manifest.Rows != 1 {
			t.Fatal("called after cancellation/deadline")
		}
	}
}

func TestT03_26InvalidMiddlePageAndCommitFailureDoNotAdvanceManifest(t *testing.T) {
	for _, storageFailure := range []bool{false, true} {
		clock := fakeBudgetClock()
		calls, commits := 0, 0
		client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 2 && !storageFailure {
				return response(200, io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":3,"list":[]}}`))), nil
			}
			return oneRowResponse(3, fmt.Sprint(calls)), nil
		}))
		c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
		r := pageRequest()
		r.PageSize = 1
		result, err := c.Collect(context.Background(), r, taskBudget(t, clock.now.Add(time.Hour), 10), collectLimits(), sinkFunc(func(_ context.Context, r ingest.PageRequest, _ ingest.Page, _ ingest.PageSummary) error {
			commits++
			if storageFailure && r.Page == 2 {
				return errors.New("fictional-secret")
			}
			return nil
		}))
		if err == nil || calls != 2 || result.Manifest.Rows != 1 || len(result.Manifest.Pages) != 1 || result.Manifest.Complete {
			t.Fatal("failed page counted or later page fetched")
		}
		wantCommits := 1
		if storageFailure {
			wantCommits = 2
		}
		if commits != wantCommits {
			t.Fatal("invalid page committed")
		}
		for e := err; e != nil; e = errors.Unwrap(e) {
			if strings.Contains(fmt.Sprintf("%v %#v", e, e), "fictional-secret") {
				t.Fatal("storage error leaked")
			}
		}
	}
}

func TestT03_26UnknownFinalEventSurvivesAcquisitionFailure(t *testing.T) {
	clock := fakeBudgetClock()
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		return response(200, io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":"01","EXTRA":null}]}}`))), nil
	}))
	c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
	r, err := c.Collect(context.Background(), pageRequest(), taskBudget(t, clock.now.Add(time.Hour), 10), collectLimits(), sinkFunc(func(context.Context, ingest.PageRequest, ingest.Page, ingest.PageSummary) error {
		t.Fatal("unknown page committed")
		return nil
	}))
	if err == nil || r.FinalFailure == nil || r.FinalFailure.Attempts != 3 || r.Manifest.Complete || r.Manifest.Rows != 0 {
		t.Fatal("final event lost")
	}
}

func TestT03_26RestartedAcquisitionBeginsAtFirstPage(t *testing.T) {
	clock := fakeBudgetClock()
	var pages []string
	client := sourceClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		pages = append(pages, r.URL.Query().Get("page"))
		return oneRowResponse(1, "01"), nil
	}))
	c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
	r := pageRequest()
	r.PageSize = 1
	sink := sinkFunc(func(context.Context, ingest.PageRequest, ingest.Page, ingest.PageSummary) error { return nil })
	for i := 0; i < 2; i++ {
		result, err := c.Collect(context.Background(), r, taskBudget(t, clock.now.Add(time.Hour), 10), collectLimits(), sink)
		if err != nil || !result.Manifest.Complete {
			t.Fatal(err)
		}
	}
	r.Page = 2
	if _, err := c.Collect(context.Background(), r, taskBudget(t, clock.now.Add(time.Hour), 10), collectLimits(), sink); err == nil {
		t.Fatal("partial resume accepted")
	}
	if !reflect.DeepEqual(pages, []string{"1", "1"}) {
		t.Fatal("new task reused old pages")
	}
}

func TestT03_18CollectorConfirmsOneEmptyFirstPage(t *testing.T) {
	clock := fakeBudgetClock()
	calls, commits := 0, 0
	client := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, io.NopCloser(strings.NewReader(emptyResponse))), nil
	}))
	c := coordinator(t, client, configBudget(), clock, &savedCooldown{}, freeAdmission())
	r, err := c.Collect(context.Background(), pageRequest(), taskBudget(t, clock.now.Add(time.Hour), 10), collectLimits(), sinkFunc(func(_ context.Context, _ ingest.PageRequest, p ingest.Page, m ingest.PageSummary) error {
		commits++
		if p.Total != 0 || len(p.Records) != 0 || m.Number != 1 || m.Count != 0 {
			t.Fatal("bad empty page")
		}
		return nil
	}))
	if err != nil || calls != 1 || commits != 1 || !r.Manifest.Complete || r.Manifest.Rows != 0 || len(r.Manifest.Pages) != 1 {
		t.Fatal("empty acquisition incomplete or extra request")
	}
}
