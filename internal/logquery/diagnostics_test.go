package logquery

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"testing"
)

func TestDetailIncludesUnifiedRequestEvidence(t *testing.T) {
	s, rec, _, now := readFixture(t)
	e := events.Event{ID: "event", EntityID: "call", Kind: events.Call, Phase: events.Finished, Outcome: events.Failed, OccurredAt: now, Diagnostics: []diagnostic.Request{{Kind: "data", Method: "GET", Endpoint: "/source", StartedAt: now, Stage: "transport", Failure: "transport", Error: "connection refused", Sent: true}}}
	var err error
	rec.Event, err = s.Sanitize(e, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	d := project(Upstream, "epoch", rec)
	if len(d.Diagnostics) != 1 || d.Diagnostics[0].Error != "connection refused" || d.Diagnostics[0].HTTPStatus != 0 {
		t.Fatal("log detail discarded request evidence")
	}
}
