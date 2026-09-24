package update

import (
	"context"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type CheckpointStore interface {
	RecoveryCheckpoint(context.Context, *control.Document, time.Time) (control.Document, error)
	RecoveryPublication(context.Context, string) (*control.Publication, error)
}
type Journal struct {
	Store CheckpointStore
	Clock ports.Clock
}

func (j Journal) SaveLocal(ctx context.Context, state ChangeResult) error {
	if j.Store == nil || j.Clock == nil || state.Prepared.Plan.Validate() != nil || state.Prepared.Manifest.Validate() != nil {
		return ErrInitialization
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	doc := control.Document(raw)
	_, err = j.Store.RecoveryCheckpoint(ctx, &doc, j.Clock.Now())
	return err
}
func (j Journal) LoadLocal(ctx context.Context, task control.Task) (ChangeResult, error) {
	if j.Store == nil {
		return ChangeResult{}, ErrInitialization
	}
	doc, err := j.Store.RecoveryCheckpoint(ctx, nil, time.Time{})
	if err != nil {
		return ChangeResult{}, err
	}
	var out ChangeResult
	if json.Unmarshal([]byte(doc), &out) != nil || out.Prepared.Plan.TaskID != task.ID {
		return ChangeResult{}, ErrRecoveryPending
	}
	// Outbox is authoritative if the process stopped between a side effect and
	// its local checkpoint. Never infer publication from a cached stage flag.
	for _, kind := range []string{"shift", "publish"} {
		event, err := j.Store.RecoveryPublication(ctx, task.ID+"-"+kind)
		if err != nil {
			return ChangeResult{}, err
		}
		if event != nil {
			if kind == "shift" {
				out.Shift = ShiftResult{Event: *event}
			} else {
				out.Event = *event
			}
		}
	}
	return out, nil
}
