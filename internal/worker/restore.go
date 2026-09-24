package worker

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type Restorer struct{ Runtime *assembly.Runtime }

func (r Restorer) Restore(ctx context.Context, state control.RecoveryState) error {
	if r.Runtime == nil || r.Runtime.Files == nil || !r.Runtime.Files.WorkerExclusive || state.Confirmed == nil {
		return control.ErrOwnership
	}
	return r.Runtime.Control.RestoreConfirmed(ctx, state.Owner, *state.Confirmed, r.Runtime.Active.Restore)
}
