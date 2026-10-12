//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestI05_B08DisplayProgressDoesNotWaitForOwnBuildFence(t *testing.T) {
	f := newRuntime(t, false)
	id := "b08-lock-progress"
	if _, err := f.wr.Control.Admit(f.e.ctx, control.Request{ID: id, IdempotencyKey: id, Mode: "initialize", RequestedBy: "synthetic", AcceptedAt: realClock{}.Now()}); err != nil {
		t.Fatal(err)
	}
	launch, err := f.wr.Control.StartAccepted(f.e.ctx, id, f.wr.ProcessID, realClock{}.Now())
	if err != nil || !launch.Started {
		t.Fatal(err)
	}
	repo := f.wr.Control.WithOwnership(launch.Owner)
	p := activity.Snapshot{Phase: "writing_table", Table: "dwd_bzkscjzlb", Instance: "M", StartedAt: realClock{}.Now()}
	p.HeartbeatAt = p.StartedAt
	if err = repo.SaveWorkProgress(f.e.ctx, p); err != nil {
		t.Fatal(err)
	}
	ctx, metrics := buildmetric.Begin(f.e.ctx)
	tx, err := repo.BeginFenced(ctx, storage.Position{Instance: storage.Main, Slot: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	p.HeartbeatAt = realClock{}.Now()
	bounded, cancel := context.WithTimeout(f.e.ctx, 2*time.Second)
	err = repo.SaveWorkProgress(bounded, p)
	cancel()
	if err != nil {
		t.Fatal("display update blocked by physical build", err)
	}
	if err = tx.Commit(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	if metrics.Snapshot().LockHeldNS <= 0 {
		t.Fatal("fenced hold not measured")
	}
	// Even if an old display packet arrived after takeover, it cannot look like
	// the new owner's progress; display records never authorize state changes.
	if _, err = f.e.connection(t, 4).Exec(f.e.ctx, "UPDATE control.tasks SET owner_generation=owner_generation+1 WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.WorkProgress(f.e.ctx, id); err != nil || got != nil {
		t.Fatal("old display token survived takeover", err)
	}
}
