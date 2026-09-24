package worker

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type Termination struct{ Runtime *assembly.Runtime }

func (t Termination) Stopped(ctx context.Context, owner control.Ownership) (bool, error) {
	if t.Runtime == nil || t.Runtime.Files == nil || !t.Runtime.Files.WorkerExclusive {
		return false, nil
	}
	proof, ok := t.Runtime.Database.(interface {
		ProcessSQLStopped(context.Context, string) (bool, error)
		OwnSQLStopped(context.Context, string) (bool, error)
	})
	if !ok {
		return false, nil
	}
	if owner.OwnerID == t.Runtime.ProcessID {
		return proof.OwnSQLStopped(ctx, owner.OwnerID)
	}
	return proof.ProcessSQLStopped(ctx, owner.OwnerID)
}
