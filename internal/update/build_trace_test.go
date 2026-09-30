package update_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestBT01_01SealingStartsOnlyAfterAllSourcesComplete(t *testing.T) {
	for _, failure := range []string{"", "source"} {
		t.Run("source_failure_"+failure, func(t *testing.T) {
			i, task, _, requests := initialization(t, failure)
			var saved []events.Event
			r, err := events.New(i.Clock, testkit.FixedRandom(1), events.SinkFunc(func(_ context.Context, e events.Event) error {
				if e.Metadata.Build == nil {
					return nil
				}
				if e.Metadata.Build.Stage == "sealing" && e.Phase == events.Started {
					seen := map[string]bool{}
					for _, request := range *requests {
						seen[strings.SplitN(request, ":", 2)[0]] = true
					}
					if len(seen) != len(ingest.Catalog()) {
						t.Error("build timing began during acquisition")
					}
				}
				saved = append(saved, e)
				return nil
			}), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			ctx := events.WithBuildTrace(events.WithRecorder(t.Context(), r, events.Identity{TaskID: task.ID}), 1, nil)
			_, err = i.Run(ctx, task)
			if failure != "" {
				if err == nil || len(saved) != 0 {
					t.Fatal("incomplete acquisition started local build timing")
				}
				return
			}
			if err != nil || len(saved) < 4 || saved[0].Metadata.Build.Stage != "sealing" || saved[1].Metadata.Build.Stage != "sealing" || saved[1].Outcome != events.Succeeded || saved[2].Metadata.Build.Stage != "prepare_target" {
				t.Fatal("seal/retention must complete before target preparation", err)
			}
		})
	}
}
