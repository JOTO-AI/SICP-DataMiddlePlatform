package observability

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
)

func TestRestoreRejectsRawIdentityAndDoesNotMapAgain(t *testing.T) {
	s, _ := NewSanitizer(4096, 256, []byte(strings.Repeat("k", 32)), nil)
	e := events.Event{ID: "event", EntityID: "entity", Kind: events.Request, Phase: events.Started, OccurredAt: time.Now()}
	safe, err := s.Sanitize(e, Details{})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := s.restore(safe.Bytes())
	if err != nil || restored.body != safe.body {
		t.Fatal("restore changed safe identity")
	}
	var env envelope
	if json.Unmarshal(safe.Bytes(), &env) != nil {
		t.Fatal("bad safe payload")
	}
	env.Event.Identity.TaskID = "Student-00019"
	bad, _ := json.Marshal(env)
	if _, err := s.restore(bad); err == nil {
		t.Fatal("raw identity injected through restore")
	}
}

func TestBT01_06BuildMetadataSurvivesPersistenceWithoutRawIdentifiers(t *testing.T) {
	s, err := NewSanitizer(4096, 256, []byte(strings.Repeat("k", 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	build := &events.BuildStage{BuildScope: events.BuildScope{Stage: "write_copy", Combination: "combination-private", Candidate: "candidate-private", Slot: 1, Generation: 2}, SessionID: "session-private", OwnerGeneration: 3, Attempt: 1}
	e := events.Event{ID: "event", EntityID: "step", Kind: events.Step, Phase: events.Started, Identity: events.Identity{TaskID: "task", StepID: "step", Instance: "M"}, OccurredAt: time.Now(), Metadata: events.Metadata{Build: build}}
	safe, err := s.Sanitize(e, Details{})
	if err != nil {
		t.Fatal(err)
	}
	if build.Combination != "combination-private" || build.SessionID != "session-private" || strings.Contains(string(safe.Bytes()), "combination-private") || strings.Contains(string(safe.Bytes()), "candidate-private") || strings.Contains(string(safe.Bytes()), "session-private") {
		t.Fatal("mutated caller metadata or disclosed raw identity")
	}
	restored, err := s.restore(safe.Bytes())
	if err != nil || restored.body != safe.body || restored.Event().Metadata.Build.OwnerGeneration != 3 {
		t.Fatal("build metadata could not be replayed")
	}
	var env envelope
	if err := json.Unmarshal(safe.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	env.Event.Metadata.Build.Candidate = "raw-candidate"
	raw, _ := json.Marshal(env)
	if _, err := s.restore(raw); err == nil {
		t.Fatal("restoration accepted a raw candidate ID")
	}
	env.Event.Metadata.Build.Candidate = s.Correlation("candidate-private")
	env.Event.Metadata.Build.SessionID = "raw-session"
	raw, _ = json.Marshal(env)
	if _, err := s.restore(raw); err == nil {
		t.Fatal("restoration accepted a raw session ID")
	}
}
