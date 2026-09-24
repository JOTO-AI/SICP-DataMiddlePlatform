package update

import (
	"context"
	"errors"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

var ErrRecoveryPending = errors.New("recovery must resolve execution and publication before scheduling")

type RecoveryReader interface {
	RecoveryState(context.Context) (control.RecoveryState, error)
}
type ActiveReader interface {
	Read(context.Context) (control.ActiveMetadata, bool, error)
}

// Termination must establish the old executor AND its actual SQL are stopped;
// a heartbeat expiry, request timeout or process discovery time is insufficient.
type Termination interface {
	Stopped(context.Context, control.Ownership) (bool, error)
}

type RecoveryDecision struct {
	State  control.RecoveryState
	Active *control.ActiveMetadata
	Action string
}
type Recovery struct {
	Store     RecoveryReader
	Active    ActiveReader
	Execution Termination
}

// Inspect never writes finished_at, clears ownership, opens slots, or starts a
// task. Only "idle" may reach new scheduling; every other decision is recovery.
func (r Recovery) Inspect(ctx context.Context) (RecoveryDecision, error) {
	out := RecoveryDecision{Action: "pending"}
	if r.Store == nil || r.Active == nil || r.Execution == nil {
		return out, ErrInitialization
	}
	state, err := r.Store.RecoveryState(ctx)
	if err != nil {
		return out, errors.Join(ErrRecoveryPending, err)
	}
	out.State = state
	actual, present, err := r.Active.Read(ctx)
	if err != nil {
		return out, errors.Join(ErrRecoveryPending, err)
	}
	if present {
		if actual.Validate() != nil {
			return out, ErrRecoveryPending
		}
		out.Active = &actual
	}
	if state.Pending != nil {
		e := state.Pending
		target, err := control.DecodeActive(e.Target)
		if err != nil || state.Task == nil || e.TaskID != state.Task.ID || e.ID != target.EventID || e.Sequence != target.Sequence || e.TaskID != target.TaskID || e.Status != "pending" {
			return out, ErrRecoveryPending
		}
		expected := state.Confirmed
		if e.Expected != nil {
			old, err := control.DecodeActive(*e.Expected)
			if err != nil || expected == nil || *expected != old {
				return out, ErrRecoveryPending
			}
		} else if expected != nil {
			return out, ErrRecoveryPending
		}
		if expected != nil && target.Sequence <= expected.Sequence {
			return out, ErrRecoveryPending
		}
		switch e.Kind {
		case "route_shift":
			if expected == nil || expected.Position.BundleID != target.Position.BundleID || expected.Position.Position.Instance == target.Position.Position.Instance {
				return out, ErrRecoveryPending
			}
		case "bundle_publish":
			if expected != nil && (expected.Position.BundleID == target.Position.BundleID || expected.Position.Position.Instance != target.Position.Position.Instance || expected.Position.Position == target.Position.Position) {
				return out, ErrRecoveryPending
			}
		default:
			return out, ErrRecoveryPending
		}
		if present && actual == target {
			out.Action = "confirm_publication"
		} else if present && expected != nil && actual == *expected || !present && expected == nil && e.Expected == nil {
			out.Action = "deliver_publication"
		} else {
			return out, ErrRecoveryPending
		}
	} else {
		if state.Confirmed == nil {
			if present {
				return out, ErrRecoveryPending
			}
		} else if !present || actual != *state.Confirmed {
			return out, ErrRecoveryPending
		}
		if state.Task == nil {
			if state.Owner.TaskID != "" {
				return out, ErrRecoveryPending
			}
			out.Action = "idle"
			return out, nil
		}
		out.Action = "inspect_local"
	}
	if state.Owner.TaskID != "" {
		if state.Task == nil || state.Owner.TaskID != state.Task.ID {
			return RecoveryDecision{State: state, Active: out.Active, Action: "pending"}, ErrRecoveryPending
		}
		stopped, err := r.Execution.Stopped(ctx, state.Owner)
		if err != nil || !stopped {
			out.Action = "pending"
			return out, errors.Join(ErrRecoveryPending, err)
		}
	}
	return out, nil
}
