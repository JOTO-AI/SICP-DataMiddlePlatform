package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type BuildTiming struct {
	BuildStartedAt time.Time  `json:"build_started_at"`
	UsableAt       *time.Time `json:"usable_at,omitempty"`
	BundleID       string     `json:"bundle_id,omitempty"`
}

// ReleaseBuildProbeSnapshot releases only the exact opaque receipt returned to
// this worker's completed authenticated probe. User snapshots have different
// tokens/conditions and are never removed by bundle-wide cleanup.
func (r Repository) ReleaseBuildProbeSnapshot(ctx context.Context, token, bundle, operation, conditions string) error {
	if r.owner == nil {
		return ErrOwnership
	}
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 32 || len(token) != 64 || bundle == "" || len(bundle) > 256 || conditions == "" || len(conditions) > 512<<10 {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		if task.Status != "success_published" {
			return ErrEntity
		}
		if err := lockSnapshots(ctx, tx); err != nil {
			return err
		}
		return exactlyOne(tx.Exec(ctx, "DELETE FROM control.query_snapshots WHERE token=$1 AND bundle_id=$2 AND operation=$3 AND conditions=$4", token, bundle, operation, conditions))
	})
}

// StartBuildTiming preserves the FIRST seal boundary across lost replies and
// recovery. Its timestamp is never synthesized from task start or import end.
func (r Repository) StartBuildTiming(ctx context.Context, at time.Time) error {
	if r.owner == nil {
		return ErrOwnership
	}
	if at.IsZero() {
		return ErrTime
	}
	raw, _ := json.Marshal(BuildTiming{BuildStartedAt: at.UTC()})
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		if task.Status != "running" || task.StartedAt == nil || at.Before(*task.StartedAt) {
			return ErrEntity
		}
		_, err = tx.Exec(ctx, "INSERT INTO control.checkpoints (task_id,name,payload,recorded_at) VALUES ($1,'build-timing',$2::jsonb,$3) ON CONFLICT (task_id,name) DO NOTHING", r.owner.TaskID, string(raw), at.UTC())
		return err
	})
}

func (r Repository) BuildTiming(ctx context.Context, taskID string) (*BuildTiming, error) {
	if taskID == "" || len(taskID) > 128 {
		return nil, ErrEntity
	}
	var result *BuildTiming
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		var raw string
		if err := tx.QueryRow(ctx, "SELECT COALESCE((SELECT payload::text FROM control.checkpoints WHERE task_id=$1 AND name='build-timing'),'null')", taskID).Scan(&raw); err != nil {
			return err
		}
		if len(raw) > 2048 || json.Unmarshal([]byte(raw), &result) != nil {
			return ErrEntity
		}
		if result != nil && (result.BuildStartedAt.IsZero() || result.UsableAt != nil && (result.UsableAt.Before(result.BuildStartedAt) || result.BundleID == "")) {
			return ErrEntity
		}
		return nil
	})
	return result, err
}

// RecordBuildUsable is called only after the authenticated four-operation
// probe. It independently rechecks publication, progress and BOTH ready copies.
// It neither publishes nor advances watermarks.
func (r Repository) RecordBuildUsable(ctx context.Context, bundle string, at time.Time) error {
	if r.owner == nil {
		return ErrOwnership
	}
	if bundle == "" || at.IsZero() {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		if task.Status != "success_published" || task.FinishedAt == nil || task.PublishedAt == nil || task.CheckedAt == nil || task.ReadyAt == nil || at.Before(*task.FinishedAt) {
			return ErrEntity
		}
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE task_id=$1 AND kind='bundle_publish' AND status='confirmed' AND applied_at IS NOT NULL AND confirmed_at IS NOT NULL AND target::jsonb->'position'->>'combination'=$2)
AND (SELECT count(DISTINCT c.instance) FROM control.candidates c JOIN control.slots s ON s.instance=c.instance AND s.slot=c.slot AND s.generation=c.generation AND s.bundle_id=c.bundle_id WHERE c.task_id=$1 AND c.bundle_id=$2 AND c.state='ready' AND s.state='ready' AND c.ready_at IS NOT NULL AND c.evidence IS NOT NULL AND c.instance IN ('M','S'))=2
AND (SELECT count(*) FROM control.source_progress WHERE task_id=$1)=14`, r.owner.TaskID, bundle).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return ErrEntity
		}
		return exactlyOne(tx.Exec(ctx, `UPDATE control.checkpoints SET payload=payload || jsonb_build_object('usable_at',$3::timestamptz,'bundle_id',$2::text),recorded_at=$3 WHERE task_id=$1 AND name='build-timing' AND (payload->>'build_started_at')::timestamptz<=$3 AND NOT (payload ? 'usable_at')`, r.owner.TaskID, bundle, at.UTC()))
	})
}
