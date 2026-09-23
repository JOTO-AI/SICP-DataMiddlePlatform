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
