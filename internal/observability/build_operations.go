package observability

import (
	"context"
	"reflect"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func BuildStages() []string {
	return []string{"business_check", "confirm_progress", "content_check", "content_table", "drain", "index_check", "input_check", "merge_table", "old_copy_check", "prepare_target", "publish", "publish_confirm", "route_shift", "sealing", "target_check", "usable_probe", "validate_copy", "write_copy", "write_table"}
}
func IsBuildStage(stage string) bool {
	for _, s := range BuildStages() {
		if stage == s {
			return true
		}
	}
	return false
}

// BuildOperation is observed telemetry. It never authorizes a control transition.
// Unknown end/start and counters remain null when the corresponding boundary
// has not arrived. Each attempt and recovered session retains its own identity.
type BuildOperation struct {
	ID              string               `json:"id"`
	Scope           events.BuildScope    `json:"scope"`
	SessionID       string               `json:"session_id"`
	Instance        string               `json:"instance"`
	OwnerGeneration int64                `json:"owner_generation,string"`
	Attempt         int                  `json:"attempt"`
	State           string               `json:"state"`
	StartedAt       *time.Time           `json:"started_at"`
	FinishedAt      *time.Time           `json:"finished_at"`
	DurationNS      *int64               `json:"duration_ns,string"`
	CumulativeNS    *int64               `json:"cumulative_ns,string"`
	Metrics         *buildmetric.Summary `json:"metrics"`
}

var buildReads = make(chan struct{}, 8)

func (d TaskDiagnostics) BuildOperations(ctx context.Context, task, stage, after string, at time.Time, limit int) ([]BuildOperation, error) {
	if d.Sanitizer == nil || d.Repository.Database == nil {
		return nil, ErrDelivery
	}
	if task == "" || len(task) > 128 || !IsBuildStage(stage) || len(after) > 128 || limit < 1 || limit > 201 || at.IsZero() {
		return nil, ErrPayload
	}
	select {
	case buildReads <- struct{}{}:
		defer func() { <-buildReads }()
	default:
		return nil, ErrLimit
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, e := d.Repository.Registry.LogReader(storage.PlatformLog)
	if e != nil {
		return nil, ErrDelivery
	}
	db, ok := d.Repository.Database.(interface {
		BeginRead(context.Context, storage.Connection) (storage.Transaction, error)
	})
	if !ok {
		return nil, ErrDelivery
	}
	tx, e := db.BeginRead(ctx, c)
	if e != nil {
		return nil, ErrDelivery
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var allowed bool
	if tx.QueryRow(ctx, `SELECT NOT has_table_privilege(current_user,'logs.events','INSERT,UPDATE,DELETE,TRUNCATE') AND NOT has_function_privilege(current_user,'logs.append_event(text,text,text,text,timestamptz,jsonb)','EXECUTE')`).Scan(&allowed) != nil || !allowed {
		return nil, ErrDelivery
	}
	q, ok := tx.(storage.QueryExecutor)
	if !ok {
		return nil, ErrDelivery
	}
	id := d.Sanitizer.Correlation(task)
	// Bound in PostgreSQL before transferring any legacy payload. Candidate
	// identities and both boundaries come from one repeatable-read snapshot.
	rows, e := q.Query(ctx, `WITH ids AS (SELECT entity_id FROM logs.events WHERE kind='step' AND payload #>> '{event,identity,task_id}'=$1 AND payload #>> '{event,metadata,build,stage}'=$2 AND entity_id>$3 AND received_at<=$4 GROUP BY entity_id ORDER BY entity_id LIMIT $5),
page AS (SELECT e.entity_id,e.phase,e.payload,octet_length(e.payload::text) AS size FROM ids JOIN logs.events e USING(entity_id) WHERE e.kind='step' AND e.received_at<=$4 ORDER BY entity_id,phase LIMIT 403)
SELECT entity_id,phase,size,CASE WHEN size<=262144 AND sum(size) OVER(ORDER BY entity_id,phase)<=2097152 THEN payload::text ELSE '' END FROM page ORDER BY entity_id,phase`, id, stage, after, at, limit)
	if e != nil {
		return nil, ErrDelivery
	}
	defer rows.Close()
	out := []BuildOperation{}
	byID := map[string]int{}
	phases := map[string]bool{}
	total := 0
	for rows.Next() {
		var entity, phase, body string
		var size int
		if rows.Scan(&entity, &phase, &size, &body) != nil {
			return nil, ErrDelivery
		}
		total += size
		if size < 1 || size > 262144 || total > 2097152 || body == "" {
			return nil, ErrLimit
		}
		safe, e := d.Sanitizer.Restore([]byte(body))
		if e != nil {
			return nil, e
		}
		event := safe.Event()
		if event.Kind != events.Step || event.Identity.TaskID != id || event.EntityID != entity || string(event.Phase) != phase || event.Metadata.Build == nil || event.Metadata.Build.Stage != stage || phases[entity+":"+phase] {
			return nil, ErrPayload
		}
		phases[entity+":"+phase] = true
		idx, found := byID[entity]
		if !found {
			if len(out) >= limit {
				return nil, ErrLimit
			}
			idx = len(out)
			byID[entity] = idx
			b := event.Metadata.Build
			out = append(out, BuildOperation{ID: entity, Scope: b.BuildScope, SessionID: b.SessionID, Instance: event.Identity.Instance, OwnerGeneration: b.OwnerGeneration, Attempt: b.Attempt, State: "unknown"})
		}
		if e = mergeBuildBoundary(&out[idx], event); e != nil {
			return nil, e
		}
	}
	if rows.Err() != nil || ctx.Err() != nil {
		return nil, ErrDelivery
	}
	rows.Close()
	if tx.Commit(ctx) != nil {
		return nil, ErrDelivery
	}
	return out, nil
}

func mergeBuildBoundary(v *BuildOperation, e events.Event) error {
	b := e.Metadata.Build
	if b == nil || !reflect.DeepEqual(v.Scope, b.BuildScope) || v.SessionID != b.SessionID || v.OwnerGeneration != b.OwnerGeneration || v.Attempt != b.Attempt || v.Instance != e.Identity.Instance {
		return ErrConflict
	}
	at := e.OccurredAt
	switch e.Phase {
	case events.Started:
		if v.StartedAt != nil {
			return ErrConflict
		}
		v.StartedAt = &at
	case events.Finished:
		if v.FinishedAt != nil {
			return ErrConflict
		}
		v.FinishedAt = &at
		v.State = string(e.Outcome)
		v.DurationNS = e.DurationNS
		cumulative, metrics := b.CumulativeNS, b.Metrics
		v.CumulativeNS = &cumulative
		v.Metrics = &metrics
	default:
		return ErrPayload
	}
	return nil
}
