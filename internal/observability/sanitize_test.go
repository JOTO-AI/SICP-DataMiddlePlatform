package observability_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
)

func event() events.Event {
	return events.Event{ID: "synthetic_event", EntityID: "synthetic_entity", Kind: events.Request, Phase: events.Started, Identity: events.Identity{RequestID: "synthetic_entity"}, OccurredAt: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)}
}
func sanitizer(t *testing.T) *observability.Sanitizer {
	t.Helper()
	s, err := observability.NewSanitizer(4096, 256, []byte(strings.Repeat("k", 32)), []string{"fictional-secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestT05_05SanitizesBeforeAllOutputBoundaries(t *testing.T) {
	secrets := []string{"fictional-secret-token", "Person-Fictional", "Student-00019", "Phone-123456789", "Mail-example.invalid", "身份证-TEST"}
	fields := map[string]string{"access_token": secrets[0], "XM": secrets[1], "XH": secrets[2], "YDDH": secrets[3], "DZXX": secrets[4], secrets[5]: secrets[1]}
	raw := observability.Details{Message: strings.Join(secrets, "\n"), Fields: fields}
	var outputs [][]byte
	guard, err := observability.NewGuard(sanitizer(t), observability.SafeSinkFunc(func(_ context.Context, e observability.SafeEvent) error {
		if !e.Valid() {
			t.Fatal("invalid safe payload")
		}
		outputs = append(outputs, e.Bytes())
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.RecordDetails(context.Background(), event(), raw); err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, []byte(fmt.Sprintf("%v %#v", raw, raw)))
	outputs = append(outputs, []byte(fmt.Sprintf("%v %#v %v %#v", sanitizer(t), sanitizer(t), guard, guard)))
	outputs = append(outputs, []byte(fmt.Sprintf("%v %#v %v %#v", *sanitizer(t), *sanitizer(t), *guard, *guard)))
	safe, err := sanitizer(t).Sanitize(event(), raw)
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, safe.Bytes(), []byte(fmt.Sprintf("%v %#v", safe, safe)))
	for _, out := range outputs {
		for _, secret := range secrets {
			if strings.Contains(string(out), secret) {
				t.Fatal("secret reached output boundary")
			}
		}
	}
	bad := event()
	bad.Identity.TaskID = secrets[0]
	safeBad, err := sanitizer(t).Sanitize(bad, observability.Details{})
	if err != nil || strings.Contains(string(safeBad.Bytes()), secrets[0]) {
		t.Fatal("metadata secret bypassed sanitizer")
	}
	for _, text := range []string{fmt.Sprintf("%v %#v", bad, bad), fmt.Sprintf("%v %#v", bad.Identity, bad.Identity)} {
		if strings.Contains(text, secrets[0]) {
			t.Fatal("formatter leaked raw identity")
		}
	}
	if (observability.SafeEvent{}).Valid() {
		t.Fatal("zero value safe payload accepted")
	}
}
func TestHMACCorrelationSurvivesRebuildAndCoversEveryIdentity(t *testing.T) {
	person := "Student-00019"
	e := event()
	e.ID = person
	e.EntityID = person
	e.Identity = events.Identity{RequestID: person, TaskID: person, StepID: person, CallID: person, AttemptID: person}
	s := sanitizer(t)
	safe, err := s.Sanitize(e, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	rebuilt := sanitizer(t)
	again, err := rebuilt.Sanitize(e, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	if string(safe.Bytes()) != string(again.Bytes()) || strings.Contains(string(safe.Bytes()), person) {
		t.Fatal("restart changed identities or leaked raw identity")
	}
	mapped := s.Correlation(person)
	out := safe.Event()
	for _, id := range []string{safe.ID(), out.EntityID, out.Identity.RequestID, out.Identity.TaskID, out.Identity.StepID, out.Identity.CallID, out.Identity.AttemptID} {
		if id != mapped {
			t.Fatal("control-to-log association diverged")
		}
	}
	if _, err := observability.NewSanitizer(4096, 256, make([]byte, 32), nil); err == nil {
		t.Fatal("zero persistent key accepted")
	}
}
func TestTypedMetadataKeepsSourceDiagnosticsAndRejectsArbitraryPaths(t *testing.T) {
	e := event()
	e.Metadata = events.Metadata{Source: "dwd_xqjbsjzlb", Path: "/apizt/openapi/invoke/sjzc/release/dwd_xqjbsjzlb", Mode: "full", Page: 2, PageSize: 50}
	safe, err := sanitizer(t).Sanitize(e, observability.Details{})
	if err != nil {
		t.Fatal(err)
	}
	if safe.Event().Metadata != e.Metadata {
		t.Fatal("useful source diagnostics lost")
	}
	e.Metadata.Path += "?token=fictional-secret-token"
	if strings.Contains(fmt.Sprintf("%v %#v %v %#v", e.Metadata, e.Metadata, &e.Metadata, &e.Metadata), "fictional-secret-token") {
		t.Fatal("metadata formatter leaked")
	}
	if _, err := sanitizer(t).Sanitize(e, observability.Details{}); err == nil {
		t.Fatal("untrusted path accepted")
	}
	e.Metadata.Path = "/apizt/openapi/invoke/sjzc/release/dwd_xqjbsjzlb"
	e.Metadata.Mode = "incremental"
	e.Metadata.EndTime = "2026-09-22 01:00:00"
	for _, fraction := range []string{strings.Repeat("1", 10), strings.Repeat("1", 100000)} {
		e.Metadata.StartTime = "2026-09-22 00:00:00." + fraction
		if _, err := sanitizer(t).Sanitize(e, observability.Details{}); err == nil {
			t.Fatal("unbounded fractional timestamp accepted")
		}
	}
}
func TestT05_06BoundedDetailsRetainTruncationAndSafeErrors(t *testing.T) {
	s := sanitizer(t)
	safe, err := s.Sanitize(event(), observability.Details{Message: strings.Repeat("secret", 10000)})
	if err != nil {
		t.Fatal(err)
	}
	if len(safe.Bytes()) > 4096 {
		t.Fatal("unbounded encoded output")
	}
	var body struct {
		Details struct {
			OriginalBytes int  `json:"original_bytes"`
			Truncated     bool `json:"truncated"`
		}
	}
	if json.Unmarshal(safe.Bytes(), &body) != nil || body.Details.OriginalBytes != 60000 || !body.Details.Truncated {
		t.Fatal("truncation evidence missing")
	}
	mutated := safe.Bytes()
	mutated[0] = 'X'
	if safe.Bytes()[0] != '{' {
		t.Fatal("safe payload externally mutable")
	}
	guard, _ := observability.NewGuard(s, observability.SafeSinkFunc(func(context.Context, observability.SafeEvent) error { return errors.New("fictional-secret-token") }))
	if err := guard.Record(context.Background(), event()); err != observability.ErrBuffer || strings.Contains(err.Error(), "fictional-secret-token") {
		t.Fatal("buffer error leaked")
	}
}
func TestT05_06BufferCannotRecursivelyRecordItself(t *testing.T) {
	var guard *observability.Guard
	calls := 0
	guard, _ = observability.NewGuard(sanitizer(t), observability.SafeSinkFunc(func(ctx context.Context, _ observability.SafeEvent) error {
		calls++
		if err := guard.Record(ctx, event()); err != observability.ErrRecursive {
			t.Fatal("recursive event accepted")
		}
		return nil
	}))
	if err := guard.Record(context.Background(), event()); err != nil || calls != 1 {
		t.Fatal("recursive buffering")
	}
}
