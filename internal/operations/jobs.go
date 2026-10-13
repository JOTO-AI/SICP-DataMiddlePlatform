package operations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type Job struct {
	ID         string          `json:"id"`
	Subject    string          `json:"-"`
	Action     string          `json:"action"`
	Request    json.RawMessage `json:"-"`
	Status     string          `json:"status"`
	AcceptedAt time.Time       `json:"accepted_at"`
	Deadline   time.Time       `json:"deadline"`
	ExpiresAt  time.Time       `json:"idempotency_expires_at"`
	Owner      *string         `json:"-"`
	LeaseUntil *time.Time      `json:"-"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	Summary    json.RawMessage `json:"summary"`
}

func NewID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

const jobColumns = "id,subject,action,request::text,status,accepted_at,deadline,expires_at,owner,lease_until,started_at,finished_at,summary::text"

func scanJob(row storage.Row) (Job, error) {
	var j Job
	var req, summary string
	if e := row.Scan(&j.ID, &j.Subject, &j.Action, &req, &j.Status, &j.AcceptedAt, &j.Deadline, &j.ExpiresAt, &j.Owner, &j.LeaseUntil, &j.StartedAt, &j.FinishedAt, &summary); e != nil {
		return Job{}, ErrUnavailable
	}
	if len(req) > 65536 || len(summary) > 16384 || !json.Valid([]byte(req)) || !json.Valid([]byte(summary)) {
		return Job{}, ErrUnavailable
	}
	j.Request = json.RawMessage(req)
	j.Summary = json.RawMessage(summary)
	return j, nil
}

// Check runs only for a new acceptance, after the idempotency lookup and while
// holding the shared admission lock. 202 is returned only after audit commits.
func (r Repository) Accept(ctx context.Context, subject, action, key string, request any, now time.Time, ttl time.Duration, check func(context.Context, storage.Transaction) error) (Job, error) {
	if subject == "" || len(subject) > 128 || !ValidID(key) || !ValidID(action) || ttl <= 0 || ttl > 24*time.Hour {
		return Job{}, ErrArgument
	}
	if action != "source_test" && action != "notification_test" && action != "cleanup" {
		return Job{}, ErrArgument
	}
	raw, e := json.Marshal(request)
	if e != nil || len(raw) > 65536 {
		return Job{}, ErrArgument
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	id := NewID()
	if id == "" {
		return Job{}, ErrUnavailable
	}
	now = now.UTC().Truncate(time.Microsecond)
	var out Job
	err := r.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(734219100)"); e != nil {
			return ErrUnavailable
		}
		var oldID, oldHash *string
		if tx.QueryRow(ctx, "SELECT (SELECT id FROM control.management_jobs WHERE subject=$1 AND action=$2 AND idempotency_key=$3),(SELECT request_hash FROM control.management_jobs WHERE subject=$1 AND action=$2 AND idempotency_key=$3)", subject, action, key).Scan(&oldID, &oldHash) != nil {
			return ErrUnavailable
		}
		if oldID != nil {
			var e error
			out, e = scanJob(tx.QueryRow(ctx, "SELECT "+jobColumns+" FROM control.management_jobs WHERE id=$1", *oldID))
			if e != nil {
				return e
			}
			if now.Before(out.ExpiresAt) || out.Status == "queued" || out.Status == "running" {
				if oldHash == nil || *oldHash != digest {
					return ErrConflict
				}
				return nil
			}
			if _, e = tx.Exec(ctx, "UPDATE control.management_jobs SET idempotency_key=NULL WHERE id=$1", out.ID); e != nil {
				return ErrUnavailable
			}
		}
		if check != nil {
			if e := check(ctx, tx); e != nil {
				return e
			}
		}
		if action == "source_test" {
			var count int
			if tx.QueryRow(ctx, "SELECT count(*) FROM control.management_jobs WHERE action='source_test' AND status='queued'").Scan(&count) != nil {
				return ErrUnavailable
			}
			if count >= 20 {
				return ErrLimit
			}
		}
		if n, e := tx.Exec(ctx, `INSERT INTO control.management_jobs(id,subject,action,idempotency_key,request_hash,request,status,accepted_at,deadline,expires_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,'queued',$7,$8,$9)`, id, subject, action, key, digest, string(raw), now, now.Add(ttl), now.Add(7*24*time.Hour)); e != nil || n != 1 {
			return ErrUnavailable
		}
		if n, e := tx.Exec(ctx, "INSERT INTO control.management_audit(id,subject,action,occurred_at) VALUES($1,$2,$3,$4)", id, subject, action, now); e != nil || n != 1 {
			return ErrUnavailable
		}
		out = Job{ID: id, Subject: subject, Action: action, Request: raw, Status: "queued", AcceptedAt: now, Deadline: now.Add(ttl), ExpiresAt: now.Add(7 * 24 * time.Hour), Summary: json.RawMessage(`{}`)}
		return nil
	})
	if err != nil {
		return Job{}, err
	}
	return out, nil
}
func (r Repository) Job(ctx context.Context, id, subject, action string) (Job, error) {
	if !ValidID(id) {
		return Job{}, ErrArgument
	}
	var out Job
	err := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		var exists bool
		if tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM control.management_jobs WHERE id=$1 AND subject=$2 AND action=$3)", id, subject, action).Scan(&exists) != nil {
			return ErrUnavailable
		}
		if !exists {
			return ErrMissing
		}
		var e error
		out, e = scanJob(tx.QueryRow(ctx, "SELECT "+jobColumns+" FROM control.management_jobs WHERE id=$1", id))
		return e
	})
	if err != nil {
		return Job{}, err
	}
	return out, nil
}
func (r Repository) Claim(ctx context.Context, action, owner string, now time.Time) (*Job, error) {
	if !ValidID(owner) {
		return nil, ErrArgument
	}
	var out *Job
	err := r.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(734219100)"); e != nil {
			return ErrUnavailable
		}
		if action == "cleanup" {
			if _, e := tx.Exec(ctx, "UPDATE control.management_jobs SET status='queued',owner=NULL,lease_until=NULL WHERE action='cleanup' AND status='running' AND lease_until<=$1", now); e != nil {
				return ErrUnavailable
			}
		}
		if _, e := tx.Exec(ctx, `UPDATE control.management_jobs SET status=CASE WHEN status='running' THEN 'interrupted' ELSE 'failed' END,finished_at=$1,summary='{"reason":"deadline_or_executor_lost","result_available":false}' WHERE action=$2 AND ((status='running' AND lease_until<=$1) OR (status='queued' AND deadline<=$1 AND NOT(action='cleanup' AND started_at IS NOT NULL)))`, now, action); e != nil {
			return ErrUnavailable
		}
		var id *string
		if tx.QueryRow(ctx, "SELECT (SELECT id FROM control.management_jobs WHERE action=$1 AND status='queued' AND (deadline>$2 OR (action='cleanup' AND started_at IS NOT NULL)) ORDER BY accepted_at,id LIMIT 1)", action, now).Scan(&id) != nil {
			return ErrUnavailable
		}
		if id == nil {
			return nil
		}
		j, e := scanJob(tx.QueryRow(ctx, "SELECT "+jobColumns+" FROM control.management_jobs WHERE id=$1", *id))
		if e != nil {
			return e
		}
		lease := j.Deadline.Add(5 * time.Second)
		if action == "cleanup" {
			lease = now.Add(30 * time.Second)
		}
		if n, e := tx.Exec(ctx, "UPDATE control.management_jobs SET status='running',owner=$2,lease_until=$3,started_at=coalesce(started_at,$4) WHERE id=$1 AND status='queued'", j.ID, owner, lease, now); e != nil || n != 1 {
			return ErrConflict
		}
		j.Status = "running"
		j.Owner = &owner
		j.LeaseUntil = &lease
		if j.StartedAt == nil {
			j.StartedAt = &now
		}
		out = &j
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (r Repository) Finish(ctx context.Context, j Job, status string, summary any, now time.Time) error {
	if status != "succeeded" && status != "failed" && status != "partial_failed" {
		return ErrArgument
	}
	raw, e := json.Marshal(summary)
	if e != nil || len(raw) > 16384 || j.Owner == nil {
		return ErrArgument
	}
	return r.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		n, e := tx.Exec(ctx, "UPDATE control.management_jobs SET status=$3,summary=$4::jsonb,finished_at=$5 WHERE id=$1 AND owner=$2 AND status='running' AND lease_until>$5", j.ID, *j.Owner, status, string(raw), now)
		if e != nil {
			return ErrUnavailable
		}
		if n != 1 {
			return ErrConflict
		}
		return nil
	})
}
