package control

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// AcquisitionProgress is display-only telemetry. It never proves a seal,
// authorizes recovery, or advances successful source watermarks.
type AcquisitionProgress struct {
	Tables    []TableAcquisitionProgress `json:"tables"`
	UpdatedAt time.Time                  `json:"updated_at"`
}

type TableAcquisitionProgress struct {
	Table          string `json:"table"`
	Status         string `json:"status"`
	CompletedPages int64  `json:"completed_pages"`
	TotalPages     *int64 `json:"total_pages"`
	Reused         bool   `json:"reused,omitempty"`
}

func (p AcquisitionProgress) valid() bool {
	catalog := source.Catalog()
	if p.UpdatedAt.IsZero() || len(p.Tables) != len(catalog) {
		return false
	}
	known := map[string]bool{}
	for _, table := range catalog {
		known[table.Name] = true
	}
	for _, table := range p.Tables {
		restoring := table.Status == "checking" || table.Status == "reusing"
		if !known[table.Table] || table.CompletedPages < 0 || (table.Reused && table.Status != "collected") {
			return false
		}
		delete(known, table.Table)
		if table.TotalPages == nil {
			if table.CompletedPages != 0 || (table.Status != "waiting" && table.Status != "acquiring" && table.Status != "failed" && !restoring) {
				return false
			}
			continue
		}
		if *table.TotalPages < 1 || table.CompletedPages > *table.TotalPages ||
			(table.Status != "acquiring" && table.Status != "collected" && table.Status != "failed" && !restoring) ||
			(table.Status == "collected" && table.CompletedPages != *table.TotalPages) ||
			(table.Status == "acquiring" && table.CompletedPages == *table.TotalPages) {
			return false
		}
	}
	return true
}

func (r Repository) SaveAcquisitionProgress(ctx context.Context, p AcquisitionProgress) error {
	if r.owner == nil {
		return ErrOwnership
	}
	if !p.valid() {
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
		if task.Status != "running" {
			return ErrEntity
		}
		return exactlyOne(tx.Exec(ctx, "INSERT INTO control.checkpoints (task_id,name,payload,recorded_at) VALUES ($1,'acquisition-progress',$2::jsonb,$3) ON CONFLICT (task_id,name) DO UPDATE SET payload=EXCLUDED.payload,recorded_at=EXCLUDED.recorded_at", r.owner.TaskID, string(raw), p.UpdatedAt))
	})
}

func (r Repository) AcquisitionProgress(ctx context.Context, taskID string) (*AcquisitionProgress, error) {
	if strings.TrimSpace(taskID) == "" || len(taskID) > 64 {
		return nil, ErrEntity
	}
	var out *AcquisitionProgress
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		var raw string
		if err := tx.QueryRow(ctx, "SELECT COALESCE((SELECT payload::text FROM control.checkpoints WHERE task_id=$1 AND name='acquisition-progress'),'null')", taskID).Scan(&raw); err != nil {
			return err
		}
		if len(raw) > 16<<10 || json.Unmarshal([]byte(raw), &out) != nil || (out != nil && !out.valid()) {
			return ErrEntity
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
