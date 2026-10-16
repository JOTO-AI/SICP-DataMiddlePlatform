package logquery

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

// Capture actual recorder boundaries through sanitation and the public DTO.
// Optional export is consumed unchanged by frontend tests, not authored there.
func TestLogContextBuildBoundaries(t *testing.T) {
	sanitizer, rec, _, now := readFixture(t)
	clock := &testkit.FakeClock{Time: now}
	var details []Detail
	r, err := events.New(clock, testkit.FixedRandom(3), events.SinkFunc(func(_ context.Context, e events.Event) error {
		safe, err := sanitizer.Sanitize(e, observability.Details{})
		if err != nil {
			return err
		}
		rec.Event, rec.Sequence, rec.ReceivedAt = safe, int64(len(details)+1), clock.Time
		details = append(details, project(Platform, "epoch-1", rec))
		return nil
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := events.WithBuildTrace(events.WithRecorder(t.Context(), r, events.Identity{TaskID: "task"}), 1, func(err error) { t.Fatal(err) })
	for _, instance := range []string{"M", "S"} {
		end := events.BeginBuild(ctx, events.BuildScope{Stage: "write_table", Table: "dwd_bzkscjzlb", Combination: "C1", Candidate: "candidate" + instance, Generation: 7, Slot: 0}, instance)
		clock.Time = clock.Time.Add(time.Second)
		end(nil)
	}
	if len(details) != 4 {
		t.Fatal("missing boundaries")
	}
	for index, d := range details {
		c := d.Context
		instance := "M"
		if index >= 2 {
			instance = "S"
		}
		if c == nil || c.Stage != "write_table" || c.Table != "dwd_bzkscjzlb" || c.Instance != instance || c.Slot == nil || *c.Slot != 0 || c.Attempt != 1 {
			t.Fatal("lost measured scope")
		}
		if index%2 == 0 && d.Outcome != nil {
			t.Fatal("start gained an outcome")
		}
	}
	// The same projection supplies lists/SSE and details, without raw metadata.
	items := make([]EventItem, len(details))
	for i := range details {
		items[i] = details[len(details)-1-i].EventItem
	}
	if path := os.Getenv("SICP_LOG_CONTEXT_FIXTURE"); path != "" {
		body, err := json.MarshalIndent(struct {
			Details []Detail    `json:"details"`
			Items   []EventItem `json:"items"`
		}{details, items}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(body, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLogContextSourceAndAbsentScope(t *testing.T) {
	s, rec, _, _ := readFixture(t)
	if project(Platform, "epoch", rec).Context != nil {
		t.Fatal("invented context")
	}
	e := events.Event{ID: "call", EntityID: "call", Kind: events.Call, Phase: events.Started, OccurredAt: rec.ReceivedAt, Metadata: events.Metadata{Source: "dwd_bzkscjzlb", Path: "/apizt/openapi/invoke/sjzc/release/dwd_bzkscjzlb", Page: 1, PageSize: 100, Mode: "full"}}
	var err error
	rec.Event, err = s.Sanitize(e, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	c := project(Upstream, "epoch", rec).Context
	if c == nil || c.Table != e.Metadata.Source || c.Page != 1 || c.Mode != "full" || c.Stage != "" || c.Slot != nil {
		t.Fatal("incorrect source context")
	}
}
