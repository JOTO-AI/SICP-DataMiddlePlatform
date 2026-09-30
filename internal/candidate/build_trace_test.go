package candidate_test

import (
	"context"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestBT01_02CandidateFailureTracePreservesReadinessRules(t *testing.T) {
	for _, failure := range []string{"write", "source", "commit", ""} {
		t.Run(failure, func(t *testing.T) {
			b, target, m, ledger, _, writer := buildFixture(t)
			var saved []events.Event
			r, err := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), events.SinkFunc(func(_ context.Context, e events.Event) error {
				saved = append(saved, e)
				return nil
			}), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			ctx := events.WithBuildTrace(events.WithRecorder(t.Context(), r, events.Identity{TaskID: "task"}), 3, nil)
			writer.fail = failure == "write"
			_, err = b.Build(ctx, target, m, true)
			if failure == "write" {
				if err == nil || ledger.complete || !ledger.failed || len(saved) != 2 || saved[1].Outcome != events.Failed {
					t.Fatal("partial write became a successful build")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			state := &validations{fail: failure}
			_, err = validator(state).Validate(ctx, target, m)
			want := events.Succeeded
			if failure != "" {
				want = events.Failed
			}
			if len(saved) != 4 || saved[3].Outcome != want || state.ready != (failure == "") || (err == nil) != (failure == "") {
				t.Fatal("trace changed candidate readiness or failure")
			}
			meta := saved[3].Metadata.Build
			if meta.Stage != "validate_copy" || meta.Combination != m.Combination || meta.Candidate != target.CandidateID || meta.Slot != target.Position.Slot || meta.Generation != target.Generation || saved[3].Identity.Instance != string(target.Position.Instance) {
				t.Fatal("physical validation scope lost")
			}
		})
	}
}
