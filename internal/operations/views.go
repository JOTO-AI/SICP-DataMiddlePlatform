package operations

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type Repository struct {
	Registry storage.Registry
	Database storage.Database
}

func (r Repository) transaction(ctx context.Context, read bool, fn func(context.Context, storage.Transaction) error) error {
	if r.Database == nil {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := r.Database.Begin(ctx, r.Registry.Control())
	if e != nil {
		return ErrUnavailable
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	if read {
		if _, e = tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); e != nil {
			return ErrUnavailable
		}
	}
	if e = fn(ctx, tx); e != nil {
		return e
	}
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return ErrUnavailable
	}
	return nil
}

type Event struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	Mode        string     `json:"mode"`
	Status      string     `json:"status"`
	Predecessor *string    `json:"predecessor_id"`
	AcceptedAt  time.Time  `json:"accepted_at"`
	StartedAt   *time.Time `json:"started_at"`
	ReadyAt     *time.Time `json:"ready_at"`
	PublishedAt *time.Time `json:"published_at"`
	CheckedAt   *time.Time `json:"checked_at"`
	FinishedAt  *time.Time `json:"finished_at"`
}
type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (r Repository) Events(ctx context.Context, before time.Time, id string, limit int) ([]Event, error) {
	if limit < 1 || limit > 201 || before.IsZero() {
		return nil, ErrArgument
	}
	out := []Event{}
	err := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrUnavailable
		}
		rows, e := q.Query(ctx, `SELECT id,mode,status,predecessor_id,accepted_at,started_at,ready_at,published_at,checked_at,finished_at FROM control.tasks WHERE accepted_at<=$1 AND ($2='' OR (accepted_at,id)<($1,$2)) ORDER BY accepted_at DESC,id DESC LIMIT $3`, before, id, limit)
		if e != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var v Event
			if rows.Scan(&v.ID, &v.Mode, &v.Status, &v.Predecessor, &v.AcceptedAt, &v.StartedAt, &v.ReadyAt, &v.PublishedAt, &v.CheckedAt, &v.FinishedAt) != nil || len(out) >= limit {
				return ErrUnavailable
			}
			v.TaskID = v.ID
			out = append(out, v)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type Operation struct {
	Build      *observability.BuildOperation `json:"build,omitempty"`
	ID         string                        `json:"id"`
	Parent     *string                       `json:"parent_operation_id"`
	Kind       string                        `json:"kind"`
	State      string                        `json:"state"`
	Evidence   string                        `json:"evidence"`
	Instance   *string                       `json:"instance"`
	Slot       *int16                        `json:"slot"`
	Generation *int64                        `json:"generation,string"`
	StartedAt  *time.Time                    `json:"started_at"`
	FinishedAt *time.Time                    `json:"finished_at"`
	Count      *int64                        `json:"count,string"`
}

// Every terminal state below comes from a control fact. Merely having a
// successful task never fabricates per-copy or per-stage success timestamps.
func (r Repository) Operations(ctx context.Context, task, parent, after string, limit int) ([]Operation, error) {
	if !ValidID(task) || limit < 1 || limit > 201 || after != "" && !ValidID(after) {
		return nil, ErrArgument
	}
	out := []Operation{}
	err := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		var exists bool
		if tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM control.tasks WHERE id=$1)", task).Scan(&exists) != nil {
			return ErrUnavailable
		}
		if !exists {
			return ErrMissing
		}
		if parent == "" {
			for _, kind := range []string{"acquisition", "builds", "cleanup", "progress", "publication", "trace"} {
				if kind > after {
					out = append(out, Operation{ID: kind, Kind: kind, State: "unknown", Evidence: "control_task"})
				}
			}
			return nil
		}
		if parent == "trace" {
			for _, stage := range observability.BuildStages() {
				id := "trace:" + stage
				if id > after {
					out = append(out, Operation{ID: id, Parent: &parent, Kind: stage, State: "unknown", Evidence: "log_diagnostics"})
				}
			}
			return nil
		}
		if strings.HasPrefix(parent, "trace:") && observability.IsBuildStage(strings.TrimPrefix(parent, "trace:")) {
			return nil
		}
		var sql string
		switch parent {
		case "builds":
			sql = `SELECT id,state,instance,slot,generation,NULL::timestamptz,ready_at FROM control.candidates WHERE task_id=$1 AND id>$2 ORDER BY id LIMIT $3`
		case "publication":
			sql = `SELECT id,status,NULL::text,NULL::smallint,NULL::bigint,created_at,confirmed_at FROM control.publication_outbox WHERE task_id=$1 AND id>$2 ORDER BY id LIMIT $3`
		case "acquisition", "progress", "cleanup":
			sql = `SELECT name,status,NULL::text,NULL::smallint,NULL::bigint,started_at,finished_at FROM control.steps WHERE task_id=$1 AND name>$2 AND name LIKE $4 ORDER BY name LIMIT $3`
		default:
			return ErrArgument
		}
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrUnavailable
		}
		args := []any{task, after, limit}
		if parent != "builds" && parent != "publication" {
			args = append(args, parent+"%")
		}
		rows, e := q.Query(ctx, sql, args...)
		if e != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			v := Operation{Parent: &parent, Kind: parent, Evidence: "control"}
			if rows.Scan(&v.ID, &v.State, &v.Instance, &v.Slot, &v.Generation, &v.StartedAt, &v.FinishedAt) != nil || len(out) >= limit {
				return ErrUnavailable
			}
			out = append(out, v)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type SourceState struct {
	ID             string                `json:"id"`
	PrimaryKey     string                `json:"primary_key"`
	ConfirmedUntil *time.Time            `json:"confirmed_until"`
	CheckedAt      *time.Time            `json:"checked_at"`
	TaskID         *string               `json:"task_id"`
	LogTaskID      *string               `json:"log_task_id"`
	Window         *control.SourceWindow `json:"window"`
	WindowTaskID   *string               `json:"window_task_id"`
}

func (r Repository) Sources(ctx context.Context, correlate func(string) string) ([]SourceState, error) {
	out := []SourceState{}
	err := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		var planRaw *string
		if tx.QueryRow(ctx, "SELECT (SELECT roles::text FROM control.tasks WHERE id=(SELECT task_id FROM control.execution WHERE singleton) AND octet_length(roles::text)<=65536)").Scan(&planRaw) != nil {
			return ErrUnavailable
		}
		var plan control.UpdatePlan
		if planRaw != nil && (json.Unmarshal([]byte(*planRaw), &plan) != nil || plan.Validate() != nil) {
			return ErrUnavailable
		}
		for _, table := range source.Catalog() {
			v := SourceState{ID: table.Name, PrimaryKey: table.PrimaryKey}
			if window, ok := plan.Windows[table.Name]; ok {
				v.Window = &window
				id := plan.TaskID
				v.WindowTaskID = &id
			}
			var raw *string
			if tx.QueryRow(ctx, "SELECT (SELECT json_build_object('confirmed_until',confirmed_until,'checked_at',checked_at,'task_id',task_id)::text FROM control.source_progress WHERE source=$1)", table.Name).Scan(&raw) != nil {
				return ErrUnavailable
			}
			if raw != nil {
				if len(*raw) > 2048 || json.Unmarshal([]byte(*raw), &v) != nil {
					return ErrUnavailable
				}
				if v.TaskID != nil && correlate != nil {
					id := correlate(*v.TaskID)
					v.LogTaskID = &id
				}
			}
			out = append(out, v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type SlotState struct {
	Instance      string  `json:"instance"`
	Slot          int16   `json:"slot"`
	Generation    int64   `json:"generation,string"`
	BundleID      *string `json:"bundle_id"`
	AdmissionOpen bool    `json:"admission_open"`
	State         string  `json:"state"`
}
type Overview struct {
	ObservedAt  time.Time               `json:"observed_at"`
	Tasks       control.TaskOverview    `json:"tasks"`
	Slots       []SlotState             `json:"slots"`
	Active      *control.ActiveMetadata `json:"active"`
	ActiveState string                  `json:"active_state"`
	Actions     map[string]string       `json:"actions"`
}

// This advisory snapshot uses the admission conditions, but never reserves
// execution. The existing mutation endpoint rechecks them under its own lock.
func (r Repository) ActionReasons(ctx context.Context) (map[string]string, error) {
	var initialized, busy bool
	var mode, status *string
	e := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM control.publication_outbox WHERE status='confirmed' AND kind='bundle_publish'),
EXISTS(SELECT 1 FROM control.execution WHERE singleton AND task_id IS NOT NULL AND task_id<>'') OR EXISTS(SELECT 1 FROM control.publication_outbox WHERE status='pending') OR EXISTS(SELECT 1 FROM control.tasks WHERE status NOT IN ('failed','success_published','success_unchanged')),
(SELECT mode FROM control.tasks ORDER BY accepted_at DESC,id DESC LIMIT 1),(SELECT status FROM control.tasks ORDER BY accepted_at DESC,id DESC LIMIT 1)`).Scan(&initialized, &busy, &mode, &status) != nil {
			return ErrUnavailable
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	out := map[string]string{"incremental": "available_recheck_on_submit", "force_full": "available_recheck_on_submit", "table_force_full": "available_recheck_on_submit", "rerun": "failed_task_selection_required"}
	if !initialized {
		out["incremental"], out["force_full"] = "initialization_required", "initialization_required"
		if mode != nil && (*mode != "initialize" || status == nil || *status != "failed") {
			out["table_force_full"] = "failed_initialization_required"
		}
	}
	if busy {
		for key := range out {
			out[key] = "task_or_publication_busy"
		}
	}
	return out, nil
}

func (r Repository) Slots(ctx context.Context) ([]SlotState, error) {
	out := []SlotState{}
	err := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrUnavailable
		}
		rows, e := q.Query(ctx, "SELECT instance,slot,generation,bundle_id,admission_open,state FROM control.slots ORDER BY instance,slot LIMIT 5")
		if e != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var v SlotState
			if rows.Scan(&v.Instance, &v.Slot, &v.Generation, &v.BundleID, &v.AdmissionOpen, &v.State) != nil || len(out) == 4 {
				return ErrUnavailable
			}
			out = append(out, v)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
