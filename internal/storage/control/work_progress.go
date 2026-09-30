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
	if r.owner == nil {
		return ErrOwnership
	}
	if !validWorkProgress(p) {
		return ErrEntity
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		if task.Status != "running" && task.Status != "published_pending_progress" {
			return ErrEntity
		}
		return exactlyOne(tx.Exec(ctx, "INSERT INTO control.checkpoints (task_id,name,payload,recorded_at) VALUES ($1,'work-progress',$2::jsonb,$3) ON CONFLICT (task_id,name) DO UPDATE SET payload=EXCLUDED.payload,recorded_at=EXCLUDED.recorded_at", r.owner.TaskID, string(raw), p.HeartbeatAt))
	})
}

func (r Repository) WorkProgress(ctx context.Context, taskID string) (*activity.Snapshot, error) {
	if strings.TrimSpace(taskID) == "" || len(taskID) > 64 {
		return nil, ErrEntity
	}
	var out *activity.Snapshot
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		var raw string
		if err := tx.QueryRow(ctx, "SELECT COALESCE((SELECT payload::text FROM control.checkpoints WHERE task_id=$1 AND name='work-progress'),'null')", taskID).Scan(&raw); err != nil {
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
