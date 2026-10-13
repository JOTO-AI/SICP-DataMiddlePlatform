package operations

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// A decision remains visible even when notifications were disabled and no
// delivery was created. Counts describe durable states, never inbox delivery.
type NotificationDecision struct {
	ID         string    `json:"id"`
	Code       string    `json:"code"`
	Decision   string    `json:"decision"`
	Revision   int64     `json:"revision,string"`
	OccurredAt time.Time `json:"occurred_at"`
	Deliveries int64     `json:"deliveries,string"`
	Accepted   int64     `json:"accepted,string"`
	Pending    int64     `json:"pending,string"`
	Unknown    int64     `json:"unknown,string"`
}

func (r Repository) Decisions(ctx context.Context, after string, limit int) ([]NotificationDecision, error) {
	if limit < 1 || limit > 201 || after != "" && !ValidID(after) {
		return nil, ErrArgument
	}
	out := []NotificationDecision{}
	e := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrUnavailable
		}
		rows, e := q.Query(ctx, `WITH facts AS (SELECT id,code,decision,revision,occurred_at FROM control.notification_facts WHERE id>$1 ORDER BY id LIMIT $2)
SELECT f.id,f.code,f.decision,f.revision,f.occurred_at,count(d.id),
count(d.id) FILTER(WHERE d.status='accepted'),count(d.id) FILTER(WHERE d.status IN ('queued','sending','retry')),
count(d.id) FILTER(WHERE d.status='unknown') FROM facts f LEFT JOIN control.notification_deliveries d ON d.fact_id=f.id
GROUP BY f.id,f.code,f.decision,f.revision,f.occurred_at ORDER BY f.id`, after, limit)
		if e != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var d NotificationDecision
			if rows.Scan(&d.ID, &d.Code, &d.Decision, &d.Revision, &d.OccurredAt, &d.Deliveries, &d.Accepted, &d.Pending, &d.Unknown) != nil || len(out) >= limit {
				return ErrUnavailable
			}
			out = append(out, d)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}
