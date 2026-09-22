package events_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func recorder(t *testing.T) (*events.Recorder, *[]events.Event, *testkit.FakeClock) {
	t.Helper()
	all := []events.Event{}
	clock := &testkit.FakeClock{Time: time.Date(2026, 9, 22, 8, 0, 0, 0, time.FixedZone("source", 8*3600))}
	r, err := events.New(clock, testkit.FixedRandom(1), events.SinkFunc(func(_ context.Context, e events.Event) error { all = append(all, e); return nil }), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return r, &all, clock
}
func TestEndRetryPreservesFirstFactAfterLostReply(t *testing.T) {
	clock := &testkit.FakeClock{Time: time.Now()}
	var ends []events.Event
	r, err := events.New(clock, testkit.FixedRandom(2), events.SinkFunc(func(_ context.Context, e events.Event) error {
		if e.Phase == events.Finished {
			ends = append(ends, e)
			if len(ends) == 1 {
				return errors.New("lost reply")
			}
		}
		return nil
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, s, err := r.Begin(context.Background(), events.Request, events.Identity{}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	clock.Time = clock.Time.Add(time.Second)
	status := 200
	if s.Finish(ctx, events.Succeeded, &status, nil) == nil {
		t.Fatal("lost reply hidden")
	}
	clock.Time = clock.Time.Add(-time.Hour)
	if s.Finish(ctx, events.Failed, &status, nil) == nil {
		t.Fatal("conflicting repeat allowed")
	}
	if err := s.Finish(ctx, events.Succeeded, &status, nil); err != nil {
		t.Fatal(err)
	}
	if len(ends) != 2 || !reflect.DeepEqual(ends[0], ends[1]) {
		t.Fatal("same event key changed its historical payload")
	}
}
func TestCancelledSourceEndHasIndependentBoundedRecordContext(t *testing.T) {
	bounded := false
	r, err := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(3), events.SinkFunc(func(ctx context.Context, e events.Event) error {
		if e.Phase != events.Finished {
			return nil
		}
		deadline, ok := ctx.Deadline()
		bounded = ok && time.Until(deadline) <= 20*time.Millisecond && ctx.Err() == nil
		<-ctx.Done()
		return ctx.Err()
	}), 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, s, err := r.Begin(ctx, events.Call, events.Identity{}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := s.Finish(ctx, events.Interrupted, nil, nil); !errors.Is(err, events.ErrRecord) || !bounded {
		t.Fatal("cancelled source lost bounded end-record attempt")
	}
}

type shortRandom struct{}

func (shortRandom) Read(p []byte) (int, error) { return len(p) - 1, nil }
func TestRandomFailureRejectsRecorder(t *testing.T) {
	if _, err := events.New(testkit.FakeClock{Time: time.Now()}, shortRandom{}, events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second); err == nil {
		t.Fatal("short random accepted")
	}
}
func TestT05_02InstanceStepsShareTaskNotStep(t *testing.T) {
	r, all, _ := recorder(t)
	ctx, task, err := r.Begin(context.Background(), events.Task, events.Identity{}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, s, err := events.Begin(ctx, events.Step, "S", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, m, err := events.Begin(ctx, events.Step, "M", 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Identity().TaskID != task.Identity().TaskID || m.Identity().TaskID != task.Identity().TaskID || s.Identity().StepID == m.Identity().StepID || len(*all) != 3 {
		t.Fatal("task/step identities lost")
	}
	if (*all)[1].Identity.Instance != "S" || (*all)[2].Identity.Instance != "M" {
		t.Fatal("instances lost")
	}
}
func TestT05_03StartDoesNotInventEndOrStatus(t *testing.T) {
	r, all, _ := recorder(t)
	_, _, err := r.Begin(context.Background(), events.Request, events.Identity{}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	e := (*all)[0]
	if e.Phase != events.Started || e.Outcome != "" || e.HTTPStatus != nil || e.BusinessCode != nil || e.DurationNS != nil || e.OccurredAt.Location() != time.UTC {
		t.Fatal("unknown end/status fabricated")
	}
}
func TestT05_04BusinessSuccessAndInterruptedTransport(t *testing.T) {
	r, all, clock := recorder(t)
	ctx, s, err := r.Begin(context.Background(), events.Request, events.Identity{}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	clock.Time = clock.Time.Add(time.Second)
	if err := s.Finish(ctx, events.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	clock.Time = clock.Time.Add(time.Second)
	status := 200
	if err := s.Transport(ctx, events.Interrupted, &status); err != nil {
		t.Fatal(err)
	}
	if len(*all) != 3 || (*all)[1].Outcome != events.Succeeded || (*all)[2].Outcome != events.Interrupted || (*all)[1].Phase != events.Finished || (*all)[2].Phase != events.Transport || *(*all)[1].DurationNS != int64(time.Second) || *(*all)[2].DurationNS != int64(2*time.Second) {
		t.Fatal("business and transport conflated")
	}
	if s.Finish(ctx, events.Failed, nil, nil) == nil {
		t.Fatal("second completion accepted")
	}
}
func TestRecordingErrorsAreSafeAndNotSuccess(t *testing.T) {
	r, _ := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return errors.New("fictional-secret") }), time.Second)
	_, _, err := r.Begin(context.Background(), events.Request, events.Identity{}, "", 0)
	if !errors.Is(err, events.ErrRecord) || err.Error() != "lifecycle event recording failed" {
		t.Fatal("unsafe record failure")
	}
}
