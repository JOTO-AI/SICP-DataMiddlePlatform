// Package worker runs one durable task at a time. Each cycle reconciles control
// and active metadata before considering the persistent schedule.
package worker

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type Store interface {
	Bootstrap(context.Context, control.Request) (control.Acceptance, error)
	CurrentSchedule(context.Context) (control.Schedule, error)
	StartAccepted(context.Context, string, string, time.Time) (control.Launch, error)
	Dispatch(context.Context, control.Request, string, time.Time, *int64) (control.Launch, error)
	Release(context.Context, control.Ownership) error
}
type Recovery interface {
	Inspect(context.Context) (update.RecoveryDecision, error)
	Claim(context.Context, string) (control.Ownership, error)
}
type Work interface {
	Run(context.Context, control.Task, control.Ownership, bool, *control.Publication) error
}
type Engine struct {
	Store    Store
	Recovery Recovery
	Work     Work
	Clock    ports.Clock
	Random   ports.Random
	Process  string
	ready    atomic.Bool
	busy     atomic.Bool
}

func (e *Engine) Ready(context.Context) bool { return e.ready.Load() }
func (e *Engine) Cycle(ctx context.Context) error {
	if !e.busy.CompareAndSwap(false, true) {
		return control.ErrBusy
	}
	defer e.busy.Store(false)
	e.ready.Store(false)
	if e.Store == nil || e.Recovery == nil || e.Work == nil || e.Clock == nil || e.Random == nil || e.Process == "" {
		return update.ErrInitialization
	}
	decision, err := e.Recovery.Inspect(ctx)
	if err != nil {
		return err
	}
	state := decision.State
	if state.Task != nil {
		task := *state.Task
		owner := state.Owner
		if task.Status == "accepted" && owner.TaskID == "" {
			launch, err := e.Store.StartAccepted(ctx, task.ID, e.Process, e.Clock.Now())
			if err != nil {
				return err
			}
			if !launch.Started {
				return update.ErrRecoveryPending
			}
			return e.Work.Run(ctx, launch.Task, launch.Owner, false, nil)
		}
		if owner.TaskID == "" {
			return update.ErrRecoveryPending
		}
		if owner.OwnerID != e.Process {
			owner, err = e.Recovery.Claim(ctx, e.Process)
			if err != nil {
				return err
			}
		}
		return e.Work.Run(ctx, task, owner, true, state.Pending)
	}
	if decision.Action != "idle" || state.Pending != nil || state.Owner.TaskID != "" {
		return update.ErrRecoveryPending
	}
	schedule, err := e.Store.CurrentSchedule(ctx)
	if err != nil {
		return err
	}
	now := e.Clock.Now()
	if state.Confirmed == nil {
		var id [16]byte
		if _, err = io.ReadFull(e.Random, id[:]); err != nil {
			return err
		}
		_, err = e.Store.Bootstrap(ctx, control.Request{ID: hex.EncodeToString(id[:]), IdempotencyKey: "system-initialize-v1", Mode: "initialize", RequestedBy: "scheduler", AcceptedAt: now})
		return err
	}
	e.ready.Store(true)
	if schedule.NextDueAt == nil || now.Before(*schedule.NextDueAt) || schedule.CooldownUntil != nil && now.Before(*schedule.CooldownUntil) {
		return nil
	}
	var id [16]byte
	if _, err = io.ReadFull(e.Random, id[:]); err != nil {
		return err
	}
	req := control.Request{ID: hex.EncodeToString(id[:]), IdempotencyKey: "timer-" + strconv.FormatInt(schedule.Revision, 10), Mode: "incremental", RequestedBy: "scheduler", AcceptedAt: now}
	launch, err := e.Store.Dispatch(ctx, req, e.Process, now, &schedule.Revision)
	if err != nil {
		return err
	}
	if !launch.Started {
		return nil
	}
	e.ready.Store(false)
	return e.Work.Run(ctx, launch.Task, launch.Owner, false, nil)
}
func (e *Engine) Run(ctx context.Context, observe func(error)) {
	for {
		if ctx.Err() != nil {
			e.ready.Store(false)
			return
		}
		err := e.Cycle(ctx)
		if err != nil && observe != nil && !errors.Is(err, context.Canceled) {
			observe(err)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			e.ready.Store(false)
			return
		case <-timer.C:
		}
	}
}
