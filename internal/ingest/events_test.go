package ingest_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT05_01RetryHasOneCallAndSeparateAttemptIdentities(t *testing.T) {
	all := []events.Event{}
	clock := &testkit.FakeClock{Time: time.Now()}
	r, _ := events.New(clock, testkit.FixedRandom(1), events.SinkFunc(func(_ context.Context, e events.Event) error { all = append(all, e); return nil }), time.Second)
	calls := 0
	c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		clock.Time = clock.Time.Add(time.Millisecond)
		if calls == 1 {
			return nil, temporaryError{}
		}
		return response(200, &trackedBody{Reader: strings.NewReader(emptyResponse)}), nil
	}))
	ctx := events.WithRecorder(context.Background(), r, events.Identity{TaskID: "task-synthetic", StepID: "step-source"})
	result, err := c.FetchPage(ctx, pageRequest(), retryPolicy())
	if err != nil || result.ObservationError != nil || len(all) != 6 {
		t.Fatalf("events: %d err=%v", len(all), err)
	}
	if all[0].Kind != events.Call || all[5].Kind != events.Call || all[0].EntityID != all[5].EntityID {
		t.Fatal("logical call duplicated")
	}
	for _, e := range all {
		if e.Metadata.Source != pageRequest().Table || e.Metadata.Page != 1 || e.Metadata.PageSize != pageRequest().PageSize {
			t.Fatal("source page metadata missing")
		}
	}
	if all[2].Metadata.Failure != "transport" || all[4].Metadata.Failure != "" {
		t.Fatal("safe failure classification missing")
	}
	if all[1].Identity.CallID != all[3].Identity.CallID || all[1].Identity.AttemptID == all[3].Identity.AttemptID || all[1].Identity.AttemptNo != 1 || all[3].Identity.AttemptNo != 2 || all[2].Outcome != events.Failed || all[4].Outcome != events.Succeeded || all[2].HTTPStatus != nil || *all[4].HTTPStatus != 200 {
		t.Fatal("retry identity/status lost")
	}
}
func TestSourceStartFailureRejectsCallAndEndFailureDoesNotUndoSuccess(t *testing.T) {
	for _, failStart := range []bool{true, false} {
		count := 0
		r, _ := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), events.SinkFunc(func(_ context.Context, e events.Event) error {
			if failStart || e.Phase == events.Finished {
				return errors.New("secret")
			}
			return nil
		}), time.Second)
		c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
			count++
			return response(200, &trackedBody{Reader: strings.NewReader(emptyResponse)}), nil
		}))
		result, err := c.FetchPage(events.WithRecorder(context.Background(), r, events.Identity{}), pageRequest(), retryPolicy())
		if failStart {
			var failure *ingest.Failure
			if !errors.As(err, &failure) || failure.Kind != ingest.ObservationFailure || count != 0 {
				t.Fatal("unlogged work admitted")
			}
		} else if err != nil || result.ObservationError == nil || count != 1 {
			t.Fatal("observation failure replaced business result")
		}
	}
}
