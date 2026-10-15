package events_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestDiagnosticReplayFreezesUnknownFailureEvidence(t *testing.T) {
	var saved []events.Event
	r, _ := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), events.SinkFunc(func(_ context.Context, e events.Event) error {
		if e.Phase == events.Finished {
			saved = append(saved, e)
			if len(saved) == 1 {
				return errors.New("lost reply")
			}
		}
		return nil
	}), time.Second)
	ctx, span, _ := r.Begin(t.Context(), events.Call, events.Identity{}, "", 0)
	requests := []diagnostic.Request{{Kind: "data", Method: "GET", Endpoint: "/source", StartedAt: time.Now().UTC(), Stage: "new_stage", Failure: "new_failure", Error: "new explanation"}}
	if span.FinishDiagnostics(ctx, events.Failed, nil, nil, "new_failure", requests) == nil {
		t.Fatal("lost reply hidden")
	}
	requests[0].Error = "mutated"
	if span.FinishDiagnostics(ctx, events.Failed, nil, nil, "new_failure", requests) == nil {
		t.Fatal("changed historical evidence accepted")
	}
	requests[0].Error = "new explanation"
	if err := span.FinishDiagnostics(ctx, events.Failed, nil, nil, "new_failure", requests); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || !reflect.DeepEqual(saved[0], saved[1]) || saved[0].Metadata.Failure != "unknown" {
		t.Fatal("immutable/unknown evidence lost")
	}
}
