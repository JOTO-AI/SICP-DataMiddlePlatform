package control_test

import (
	"sync"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestBootstrapConcurrentWorkersAdmitOnlyOnceAndDoNotRestartFailedTask(t *testing.T) {
	r, d, req := taskRepository(t)
	req.Mode, req.RequestedBy, req.IdempotencyKey = "initialize", "scheduler", "system-initialize-v1"
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if _, err := r.Bootstrap(t.Context(), req); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if len(d.records) != 1 {
		t.Fatal("bootstrap duplicated", len(d.records))
	}
	d.records[req.IdempotencyKey][3] = "failed"
	req.ID, req.IdempotencyKey = "other", "other"
	if a, err := r.Bootstrap(t.Context(), req); err != nil || a.Created || len(d.records) != 1 {
		t.Fatal("failed bootstrap retried", a, err)
	}
}

func TestBootstrapRefusesExistingEvidenceAndStoreErrors(t *testing.T) {
	for _, state := range []string{"published", "pending", "schedule", "owner", "read_failure", "commit_failure"} {
		t.Run(state, func(t *testing.T) {
			r, d, req := taskRepository(t)
			req.Mode, req.RequestedBy = "initialize", "scheduler"
			switch state {
			case "published":
				d.initialized = true
			case "pending":
				d.pending = true
			case "schedule":
				d.revision = 1
			case "owner":
				d.owner = control.Ownership{Generation: 1}
			case "read_failure":
				d.failRead = true
			case "commit_failure":
				d.failCommit = true
			}
			a, err := r.Bootstrap(t.Context(), req)
			if a.Created || len(d.records) != 0 {
				t.Fatal("non-pristine environment admitted", a)
			}
			if (state == "read_failure" || state == "commit_failure") && err == nil {
				t.Fatal("lost database error")
			}
		})
	}
}
