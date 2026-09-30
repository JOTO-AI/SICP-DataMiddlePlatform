package control_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestInitializationRecoveryPreservesHistoryAndSerializesWorkers(t *testing.T) {
	r, d, old := taskRepository(t)
	old.Mode = "initialize"
	if _, err := r.Admit(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	d.records[old.IdempotencyKey][3] = "failed"
	d.records[old.IdempotencyKey][15] = `{"failure":"corrupt_seal"}`
	var group sync.WaitGroup
	for i := range 8 {
		group.Go(func() {
			req := control.Request{ID: fmt.Sprintf("recovered-%d", i), IdempotencyKey: "unused", Mode: "initialize", RequestedBy: "scheduler", AcceptedAt: old.AcceptedAt.Add(control.InitializationRetryInterval)}
			if _, err := r.RecoverInitialization(t.Context(), req); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if len(d.records) != 2 || d.records[old.IdempotencyKey][3] != "failed" || d.records[old.IdempotencyKey][11] != nil {
		t.Fatal("history was replaced or recovery duplicated")
	}
	child := d.records["system-initialize-retry-"+old.ID]
	if child == nil || child[2] != "initialize" || child[3] != "accepted" || child[4] != "scheduler" || child[5] != old.ID {
		t.Fatal("missing linked automatic initialization", child)
	}
}

func TestInitializationRecoveryGatesAndFailureRollback(t *testing.T) {
	for _, scenario := range []string{"eligible", "acquisition", "interrupted", "empty", "unknown", "running", "incremental", "recent", "finished_recently", "cooldown", "owner", "pending", "published", "read_error", "commit_error"} {
		t.Run(scenario, func(t *testing.T) {
			r, d, old := taskRepository(t)
			old.Mode = "initialize"
			if scenario != "empty" {
				if _, err := r.Admit(t.Context(), old); err != nil {
					t.Fatal(err)
				}
				d.records[old.IdempotencyKey][3] = "failed"
				d.records[old.IdempotencyKey][15] = `{"failure":"corrupt_seal"}`
			}
			req := control.Request{ID: "recovered", IdempotencyKey: "unused", Mode: "initialize", RequestedBy: "scheduler", AcceptedAt: old.AcceptedAt.Add(10 * time.Minute)}
			switch scenario {
			case "acquisition":
				d.records[old.IdempotencyKey][15] = `{"failure":"acquisition_failed"}`
			case "interrupted":
				d.records[old.IdempotencyKey][15] = `{"failure":"interrupted"}`
			case "unknown":
				d.records[old.IdempotencyKey][15] = `{}`
			case "running":
				d.records[old.IdempotencyKey][3] = "running"
			case "incremental":
				d.records[old.IdempotencyKey][2] = "incremental"
			case "recent":
				req.AcceptedAt = old.AcceptedAt.Add(time.Minute)
			case "finished_recently":
				d.records[old.IdempotencyKey][11] = req.AcceptedAt.Add(-time.Minute)
			case "cooldown":
				until := req.AcceptedAt.Add(time.Minute)
				d.cooldown = &until
			case "owner":
				d.owner = control.Ownership{TaskID: old.ID, OwnerID: "old", Generation: 1}
			case "pending":
				d.pending = true
			case "published":
				d.initialized = true
			case "read_error":
				d.failRead = true
			case "commit_error":
				d.failCommit = true
			}
			before := len(d.records)
			got, err := r.RecoverInitialization(t.Context(), req)
			want := scenario == "eligible" || scenario == "acquisition" || scenario == "interrupted"
			if want {
				if err != nil || !got.Created || len(d.records) != before+1 {
					t.Fatal(got, err)
				}
			} else if got.Created || len(d.records) != before {
				t.Fatal("blocked recovery changed history", got, err)
			}
			if (scenario == "read_error" || scenario == "commit_error") && err == nil {
				t.Fatal("storage error ignored")
			}
		})
	}
}
