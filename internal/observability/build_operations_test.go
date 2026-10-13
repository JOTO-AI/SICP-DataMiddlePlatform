package observability

import (
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
)

func TestT31_02TraceBoundariesDoNotInventCompletionOrMergeRecoveredSessions(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	b := events.BuildStage{BuildScope: events.BuildScope{Stage: "write_copy", Candidate: "candidate", Slot: 1, Generation: 4}, SessionID: "session-1", OwnerGeneration: 2, Attempt: 1}
	v := BuildOperation{ID: "step", Scope: b.BuildScope, SessionID: b.SessionID, OwnerGeneration: 2, Attempt: 1, State: "unknown", Instance: "M"}
	e := events.Event{Phase: events.Started, OccurredAt: now, Identity: events.Identity{Instance: "M"}, Metadata: events.Metadata{Build: &b}}
	if err := mergeBuildBoundary(&v, e); err != nil || v.StartedAt == nil || v.FinishedAt != nil || v.Metrics != nil || v.State != "unknown" {
		t.Fatal("invented terminal fact", err)
	}
	b.SessionID = "session-2"
	e.Phase = events.Finished
	e.Outcome = events.Succeeded
	e.OccurredAt = now.Add(time.Second)
	if mergeBuildBoundary(&v, e) != ErrConflict || v.FinishedAt != nil {
		t.Fatal("mixed recovery sessions")
	}
	b.SessionID = "session-1"
	b.Metrics.RowsWritten = 7
	if err := mergeBuildBoundary(&v, e); err != nil || v.FinishedAt == nil || v.Metrics.RowsWritten != 7 || v.State != "succeeded" {
		t.Fatal(err)
	}
	if mergeBuildBoundary(&v, e) != ErrConflict {
		t.Fatal("duplicate terminal boundary")
	}
	if v.DurationNS != nil {
		t.Fatal("fabricated duration from wall timestamps")
	}
}
