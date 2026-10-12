package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
)

func TestT28_05AuditAndTestIdentityValidation(t *testing.T) {
	base := events.Event{ID: "event", EntityID: "entity", Kind: events.Audit, Phase: events.Finished, Outcome: events.Succeeded, OccurredAt: time.Now(), Audit: &events.AuditIdentity{ActorID: "trusted_actor", Action: "source_test", ObjectID: "test_run"}}
	if base.Validate() != nil {
		t.Fatal("valid audit rejected")
	}
	for _, mutate := range []func(*events.Event){
		func(e *events.Event) { e.Audit = nil },
		func(e *events.Event) { e.Kind = events.Request },
		func(e *events.Event) { e.Phase = events.Started; e.Outcome = "" },
		func(e *events.Event) { e.Audit = &events.AuditIdentity{Action: "cleanup"} },
		func(e *events.Event) { e.Audit = &events.AuditIdentity{ActorID: "actor", Action: "arbitrary"} },
		func(e *events.Event) {
			e.Audit = &events.AuditIdentity{ActorID: "a@example.invalid", Action: "cleanup"}
		},
	} {
		e := base
		mutate(&e)
		if e.Validate() == nil {
			t.Fatal("invalid audit accepted")
		}
	}
	base.Kind, base.Audit = events.Call, nil
	for _, id := range []events.Identity{{Purpose: events.TestPurpose}, {Purpose: events.SyncPurpose, TestRunID: "run"}, {Purpose: "other"}, {TestRunID: "run"}, {Purpose: events.TestPurpose, TestRunID: "run", TaskID: "task"}} {
		base.Identity = id
		if base.Validate() == nil {
			t.Fatal("invalid test identity accepted")
		}
	}
	base.Identity = events.Identity{Purpose: events.TestPurpose, TestRunID: "run"}
	if base.Validate() != nil {
		t.Fatal("test identity rejected")
	}
}

func TestT28_05RecorderCarriesTestIdentityAndDefaultsSync(t *testing.T) {
	r, all, _ := recorder(t)
	ctx, call, err := r.Begin(context.Background(), events.Call, events.Identity{Purpose: events.TestPurpose, TestRunID: "run"}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, attempt, err := events.Begin(ctx, events.Attempt, "", 1)
	if err != nil || call.Identity().TestRunID != "run" || attempt.Identity().Purpose != events.TestPurpose {
		t.Fatal("test association lost")
	}
	if attempt.Finish(ctx, events.Succeeded, nil, nil) != nil {
		t.Fatal("finish")
	}
	_, sync, err := r.Begin(context.Background(), events.Call, events.Identity{}, "", 0)
	if err != nil || sync.Identity().Purpose != events.SyncPurpose {
		t.Fatal("sync purpose omitted")
	}
	if len(*all) != 4 {
		t.Fatal("unexpected lifecycle count")
	}
}
