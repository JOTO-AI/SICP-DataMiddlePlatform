package control_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestT02_10ConcurrentExecutorsHaveExactlyOneWinner(t *testing.T) {
	r, d, req := taskRepository(t)
	if _, err := r.Accept(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		owner control.Ownership
		err   error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for _, id := range []string{"worker-A", "worker-B"} {
		go func() { <-start; owner, err := r.Acquire(t.Context(), req.ID, id); results <- outcome{owner, err} }()
	}
	close(start)
	winners := 0
	busy := 0
	var winner control.Ownership
	for range 2 {
		got := <-results
		if got.err == nil {
			winners++
			winner = got.owner
		} else if errors.Is(got.err, control.ErrBusy) {
			busy++
		} else {
			t.Fatal(got.err)
		}
	}
	if winners != 1 || busy != 1 || winner.Generation != 1 || d.owner != winner || d.records[req.IdempotencyKey][13] != int64(1) {
		t.Fatal("ownership split between executors")
	}
	again, err := r.Acquire(t.Context(), req.ID, winner.OwnerID)
	if err != nil || again != winner || d.records[req.IdempotencyKey][7] != nil || d.revision != 0 {
		t.Fatal("duplicate acquisition started/rescheduled or changed generation")
	}
	actualStart := req.AcceptedAt.Add(10 * time.Minute)
	if got, err := r.Start(t.Context(), winner, actualStart); err != nil || got != actualStart || d.revision != 1 {
		t.Fatal("acquired ownership did not connect to actual start", err)
	}
}

func TestT02_11OldGenerationCannotChangeCandidateOrProgress(t *testing.T) {
	r, d, req := taskRepository(t)
	if _, err := r.Accept(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	first, err := r.Acquire(t.Context(), req.ID, "worker-A")
	if err != nil {
		t.Fatal(err)
	}
	candidate := &control.Candidate{ID: "candidate-A", TaskID: req.ID, Instance: "S", Generation: 1, BundleID: "C100", OwnerGeneration: first.Generation, State: "building"}
	progress := &control.Progress{Source: "dwd_xqjbsjzlb", TaskID: req.ID, ConfirmedUntil: req.AcceptedAt, CheckedAt: req.AcceptedAt}
	old := r.WithOwnership(first)
	if err := old.Insert(t.Context(), candidate); err != nil {
		t.Fatal(err)
	}
	if err := old.Insert(t.Context(), progress); err != nil {
		t.Fatal(err)
	}
	if err := r.Release(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := r.Release(t.Context(), first); err != nil {
		t.Fatal("repeat release", err)
	}
	second, err := r.Acquire(t.Context(), req.ID, "worker-B")
	if err != nil || second.Generation != first.Generation+1 {
		t.Fatal("generation was reused", err)
	}
	for _, entity := range []any{candidate, progress} {
		if err := old.Update(t.Context(), entity); !errors.Is(err, control.ErrOwnership) {
			t.Fatal("old generation modified a protected entity", err)
		}
		if err := r.Update(t.Context(), entity); !errors.Is(err, control.ErrOwnership) {
			t.Fatal("unfenced write bypassed ownership")
		}
	}
	if len(d.mutations) != 2 || d.owner != second {
		t.Fatal("rejected mutations changed persisted state")
	}
	if err := r.Release(t.Context(), first); !errors.Is(err, control.ErrOwnership) || d.owner != second {
		t.Fatal("late release cleared newer owner")
	}
	candidate.OwnerGeneration = second.Generation
	candidate.ID = "candidate-B"
	current := r.WithOwnership(second)
	if err := current.Insert(t.Context(), candidate); err != nil {
		t.Fatal(err)
	}
	if err := current.Update(t.Context(), candidate); err != nil {
		t.Fatal(err)
	}
	last := d.mutations[len(d.mutations)-1]
	if !strings.Contains(last.sql, `WHERE "id"=`) || !strings.Contains(last.sql, `AND "owner_generation"=`) || last.args[len(last.args)-1] != second.Generation {
		t.Fatal("candidate update omitted its persisted generation condition")
	}
	if err := current.Update(t.Context(), progress); err != nil {
		t.Fatal("current owner rejected", err)
	}
	progress.TaskID = "unrelated-task"
	if err := current.Update(t.Context(), progress); !errors.Is(err, control.ErrOwnership) {
		t.Fatal("ownership crossed task identities")
	}
}

func TestT02_10OwnershipFailureRollsBackBothRecords(t *testing.T) {
	r, d, req := taskRepository(t)
	if _, err := r.Accept(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	d.failSQL = "UPDATE control.tasks SET owner_id=$1"
	if owner, err := r.Acquire(t.Context(), req.ID, "worker-A"); err == nil || owner.Generation != 0 || d.owner.TaskID != "" || d.owner.Generation != 0 || d.records[req.IdempotencyKey][12] != nil {
		t.Fatal("partial acquisition escaped rollback")
	}
	d.failSQL = ""
	owner, err := r.Acquire(t.Context(), req.ID, "worker-A")
	if err != nil {
		t.Fatal(err)
	}
	d.failSQL = "UPDATE control.execution SET task_id=NULL"
	if err := r.Release(t.Context(), owner); err == nil || d.owner != owner || d.records[req.IdempotencyKey][12] != owner.OwnerID {
		t.Fatal("partial release escaped rollback")
	}
	d.failSQL = ""
	d.failCommit = true
	if err := r.Release(t.Context(), owner); err == nil || d.owner != owner {
		t.Fatal("failed release commit reported success")
	}
}

func TestT02_11ExecutionFenceCannotBeOverwrittenThroughEntityMapping(t *testing.T) {
	r, d := repository(t)
	entity := &control.Execution{Singleton: true, TaskID: ptr("task-A"), OwnerID: ptr("owner-A"), Generation: 9}
	if err := r.Insert(t.Context(), entity); !errors.Is(err, control.ErrOwnership) {
		t.Fatal("generic insert bypassed execution ownership")
	}
	if err := r.Update(t.Context(), entity); !errors.Is(err, control.ErrOwnership) || len(d.connections) != 0 {
		t.Fatal("generic update bypassed execution ownership")
	}
	d.raw = []any{true, "task-A", "owner-A", int64(9)}
	loaded := &control.Execution{Singleton: true}
	if err := r.Load(t.Context(), loaded); err != nil || loaded.Generation != 9 || loaded.TaskID == nil || *loaded.TaskID != "task-A" || loaded.OwnerID == nil || *loaded.OwnerID != "owner-A" {
		t.Fatal("execution status could not be read", err)
	}
}
