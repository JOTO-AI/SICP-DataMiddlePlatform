package control

import (
	"context"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func knownTable(name string) bool {
	for _, table := range source.Catalog() {
		if table.Name == name {
			return true
		}
	}
	return false
}

func (r Repository) AdmitTable(ctx context.Context, request Request) (Acceptance, error) {
	if !knownTable(request.Table) || request.PredecessorID != nil {
		return Acceptance{}, ErrEntity
	}
	request.Mode = "force_full"
	return r.admit(ctx, request, false, false, false)
}

func tableSelection(ctx context.Context, tx storage.Transaction, id string) (string, error) {
	var table string
	err := tx.QueryRow(ctx, "SELECT COALESCE((SELECT payload->>'table' FROM control.checkpoints WHERE task_id=$1 AND name='table-selection'),'')", id).Scan(&table)
	if err == nil && table != "" && !knownTable(table) {
		return "", ErrEntity
	}
	return table, err
}
func (r Repository) TableSelection(ctx context.Context, id string) (string, error) {
	var table string
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		var err error
		table, err = tableSelection(ctx, tx, id)
		return err
	})
	return table, err
}

// SourceEpoch preserves a conservative lower bound across initialization
// repairs, so reusing old full tables cannot skip later incremental changes.
func (r Repository) SourceEpoch(ctx context.Context, id string) (*time.Time, error) {
	var at *time.Time
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		return tx.QueryRow(ctx, "SELECT (SELECT (payload->>'at')::timestamptz FROM control.checkpoints WHERE task_id=$1 AND name='source-epoch')", id).Scan(&at)
	})
	return at, err
}
func (r Repository) SaveSourceEpoch(ctx context.Context, at time.Time) error {
	if r.owner == nil || at.IsZero() {
		return ErrEntity
	}
	raw, _ := json.Marshal(struct {
		At time.Time `json:"at"`
	}{at})
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		task, err := loadTask(ctx, tx, r.owner.TaskID)
		if err != nil {
			return err
		}
		if task.Mode != "initialize" || task.Status != "running" || task.StartedAt == nil || at.After(*task.StartedAt) {
			return ErrTime
		}
		_, err = tx.Exec(ctx, "INSERT INTO control.checkpoints (task_id,name,payload,recorded_at) VALUES ($1,'source-epoch',$2::jsonb,$3) ON CONFLICT (task_id,name) DO NOTHING", task.ID, string(raw), at)
		if err != nil {
			return err
		}
		var saved time.Time
		if err = tx.QueryRow(ctx, "SELECT (payload->>'at')::timestamptz FROM control.checkpoints WHERE task_id=$1 AND name='source-epoch'", task.ID).Scan(&saved); err != nil {
			return err
		}
		if !saved.Equal(at) {
			return ErrIdempotency
		}
		return nil
	})
}
