package control

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func validWorkProgress(p activity.Snapshot) bool {
	if len(p.Tables) > 4 || (len(p.Tables) > 0 && (p.Phase != "building_table" || p.Table != "")) {
		return false
	}
	seen := map[string]bool{}
	for _, table := range p.Tables {
		if !knownTable(table) || seen[table] {
			return false
		}
		seen[table] = true
	}
	if p.StartedAt.IsZero() || p.HeartbeatAt.Before(p.StartedAt) || (p.Table != "" && !knownTable(p.Table)) || (p.Instance != "" && p.Instance != "M" && p.Instance != "S") {
		return false
	}
	switch p.Phase {
	case "preparing", "recovering", "acquiring", "sealing", "validating_input", "building_table", "validating_target", "writing_table", "validating_copy", "checking_unchanged", "publishing", "confirming_progress":
		return true
	}
	return false
}

func (r Repository) SaveWorkProgress(ctx context.Context, p activity.Snapshot) error {
	if r.owner == nil || validateOwnership(*r.owner) != nil {
		return ErrOwnership
	}
	if !validWorkProgress(p) {
		return ErrEntity
	}
	raw, err := json.Marshal(struct {
		activity.Snapshot
		OwnerGeneration int64 `json:"owner_generation"`
	}{p, r.owner.Generation})
	if err != nil {
		return err
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		// Display-only telemetry must not wait on the execution/slot locks held
		// by its own import. This optimistic token is never transition evidence.
		var taskID, ownerID *string
		var generation int64
		if err := tx.QueryRow(ctx, "SELECT task_id,owner_id,generation FROM control.execution WHERE singleton=TRUE").Scan(&taskID, &ownerID, &generation); err != nil {
			return err
		}
		if taskID == nil || ownerID == nil || *taskID != r.owner.TaskID || *ownerID != r.owner.OwnerID || generation != r.owner.Generation {
			return ErrOwnership
		}
		d, _ := describe(&Task{})
		loaded, err := readEntity(ctx, tx, d, `"id"=$1`, []any{r.owner.TaskID}, false)
		if err != nil {
			return err
		}
		task := loaded.Interface().(Task)
		if task.Status != "running" && task.Status != "published_pending_progress" {
			return ErrEntity
		}
		return exactlyOne(tx.Exec(ctx, `INSERT INTO control.checkpoints (task_id,name,payload,recorded_at)
SELECT $1,'work-progress',$2::jsonb,$3 WHERE EXISTS (SELECT 1 FROM control.execution WHERE singleton=TRUE AND task_id=$1 AND owner_id=$5 AND generation=$4)
ON CONFLICT (task_id,name) DO UPDATE SET payload=EXCLUDED.payload,recorded_at=EXCLUDED.recorded_at
WHERE COALESCE((control.checkpoints.payload->>'owner_generation')::bigint,0)<$4 OR
(COALESCE((control.checkpoints.payload->>'owner_generation')::bigint,0)=$4 AND control.checkpoints.recorded_at<=$3)`, r.owner.TaskID, string(raw), p.HeartbeatAt, r.owner.Generation, r.owner.OwnerID))
	})
}

func (r Repository) WorkProgress(ctx context.Context, taskID string) (*activity.Snapshot, error) {
	if strings.TrimSpace(taskID) == "" || len(taskID) > 64 {
		return nil, ErrEntity
	}
	var out *activity.Snapshot
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		var raw string
		if err := tx.QueryRow(ctx, "SELECT COALESCE((SELECT payload::text FROM control.checkpoints WHERE task_id=$1 AND name='work-progress' AND (NOT (payload ? 'owner_generation') OR (payload->>'owner_generation')::bigint=(SELECT owner_generation FROM control.tasks WHERE id=$1))),'null')", taskID).Scan(&raw); err != nil {
			return err
		}
		if len(raw) > 4096 || json.Unmarshal([]byte(raw), &out) != nil || (out != nil && !validWorkProgress(*out)) {
			return ErrEntity
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
