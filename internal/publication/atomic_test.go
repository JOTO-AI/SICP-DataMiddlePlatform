package publication_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type atomicCall struct {
	command string
	args    []any
}

// Models Redis's atomic compare/replace contract while recording the actual
// EVAL command and complete arguments emitted by the production adapter.
type atomicMemory struct {
	raw             string
	writes          int
	calls           []atomicCall
	lost, malformed bool
}

func (m *atomicMemory) Do(_ context.Context, command string, args ...any) (any, error) {
	m.calls = append(m.calls, atomicCall{command, args})
	if command == "GET" {
		if m.raw == "" {
			return nil, nil
		}
		return m.raw, nil
	}
	if command != "EVAL" || len(args) != 7 {
		return nil, errors.New("not one atomic command")
	}
	expected, target, old, next := args[3].(string), args[4].(string), args[5].(string), args[6].(string)
	outcome := "conflict"
	if m.raw == target {
		outcome = "already_applied"
	} else if m.raw == expected && (len(next) > len(old) || len(next) == len(old) && next > old) {
		m.raw = target
		m.writes++
		outcome = "applied"
	}
	if m.lost {
		return nil, context.DeadlineExceeded
	}
	if m.malformed {
		return 7, nil
	}
	return []byte(outcome), nil
}
func event(t *testing.T) (control.Publication, control.ActiveMetadata) {
	t.Helper()
	old := control.ActiveMetadata{Sequence: 9007199254740993, EventID: "old", TaskID: "old-task", OwnerGeneration: 2, Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 3, BundleID: "C100"}, CodeVersion: "query-v1"}
	expected, err := control.ActiveDocument(old)
	if err != nil {
		t.Fatal(err)
	}
	next := old
	next.Sequence++
	next.EventID = "shift"
	next.TaskID = "task"
	next.OwnerGeneration = 3
	next.Position.Position.Instance = storage.Standby
	next.Position.Generation = 4
	target, err := control.ActiveDocument(next)
	if err != nil {
		t.Fatal(err)
	}
	return control.Publication{ID: next.EventID, TaskID: next.TaskID, Sequence: next.Sequence, Kind: "route_shift", Expected: &expected, Target: target, Status: "pending", CreatedAt: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}, old
}

func TestT14_09RestorationOnlyFillsMissingMetadata(t *testing.T) {
	e, old := event(t)
	memory := &atomicMemory{}
	atomic := publication.Atomic{Client: memory}
	if err := atomic.Restore(t.Context(), old); err != nil || memory.writes != 1 || memory.raw != string(*e.Expected) {
		t.Fatal("missing confirmed route not restored", err)
	}
	if err := atomic.Restore(t.Context(), old); err != nil || memory.writes != 1 {
		t.Fatal("restoration not idempotent", err)
	}
	memory.raw = string(e.Target)
	if err := atomic.Restore(t.Context(), old); !errors.Is(err, publication.ErrConflict) || memory.raw != string(e.Target) || memory.writes != 1 {
		t.Fatal("restoration overwrote live metadata", err)
	}
}
func TestT12_09AtomicWholeMetadataSwapIsExactIdempotentAndRejectsLateEvents(t *testing.T) {
	e, old := event(t)
	memory := &atomicMemory{raw: string(*e.Expected)}
	atomic := publication.Atomic{Client: memory}
	if err := atomic.Apply(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if len(memory.calls) != 1 || memory.calls[0].command != "EVAL" || memory.writes != 1 || memory.raw != string(e.Target) {
		t.Fatal(memory)
	}
	call := memory.calls[0]
	if call.args[1] != 1 || call.args[2] != publication.ActiveKey || call.args[3] != string(*e.Expected) || call.args[4] != string(e.Target) || call.args[5] != "9007199254740993" || call.args[6] != "9007199254740994" {
		t.Fatal(call)
	}
	script := call.args[0].(string)
	for _, fragment := range []string{"current == ARGV[2]", "current ~= ARGV[1]", "#next < #old", "redis.call('SET', KEYS[1], ARGV[2])"} {
		if !strings.Contains(script, fragment) {
			t.Fatal(script)
		}
	}
	if strings.Contains(script, "tonumber") || strings.Contains(script, "EXPIRE") {
		t.Fatal(script)
	}
	if err := atomic.Apply(t.Context(), e); err != nil || memory.writes != 1 {
		t.Fatal(err, memory.writes)
	}
	doc, _ := control.ActiveDocument(old)
	late := control.Publication{ID: old.EventID, TaskID: old.TaskID, Sequence: old.Sequence, Kind: "bundle_publish", Target: doc, Status: "pending"}
	if err := atomic.Apply(t.Context(), late); !errors.Is(err, publication.ErrConflict) || memory.writes != 1 {
		t.Fatal(err)
	}
	got, present, err := atomic.Read(t.Context())
	if err != nil || !present || got.Sequence != e.Sequence || got.Position.Position.Instance != storage.Standby {
		t.Fatal(got, present, err)
	}
}
func TestT12_09WrongExpectedGenerationAndPartialResponseCannotOverwrite(t *testing.T) {
	e, old := event(t)
	memory := &atomicMemory{raw: string(*e.Expected)}
	atomic := publication.Atomic{Client: memory}
	old.Position.Generation++
	wrong, _ := control.ActiveDocument(old)
	e.Expected = &wrong
	if err := atomic.Apply(t.Context(), e); !errors.Is(err, publication.ErrConflict) || memory.writes != 0 {
		t.Fatal(err, memory)
	}
	e, _ = event(t)
	memory.malformed = true
	if err := atomic.Apply(t.Context(), e); !errors.Is(err, publication.ErrUnknown) {
		t.Fatal(err)
	}
}
func TestT12_08AppliedButLostReplyIsUnknownAndRetryDoesNotWriteAgain(t *testing.T) {
	e, _ := event(t)
	memory := &atomicMemory{raw: string(*e.Expected), lost: true}
	atomic := publication.Atomic{Client: memory}
	if err := atomic.Apply(t.Context(), e); !errors.Is(err, publication.ErrUnknown) || memory.raw != string(e.Target) {
		t.Fatal(err, memory)
	}
	memory.lost = false
	if err := atomic.Apply(t.Context(), e); err != nil || memory.writes != 1 {
		t.Fatal(err, memory)
	}
}
