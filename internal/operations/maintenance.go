package operations

import (
	"context"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type Retention struct {
	Revision     int64 `json:"revision,string"`
	PlatformDays int   `json:"platform_days"`
	UpstreamDays int   `json:"upstream_days"`
}

func (s Retention) Valid() bool {
	return s.Revision > 0 && s.PlatformDays >= 1 && s.PlatformDays <= 3650 && s.UpstreamDays >= 1 && s.UpstreamDays <= 3650
}
func retention(ctx context.Context, tx storage.Transaction, lock bool) (Retention, error) {
	var s Retention
	sql := "SELECT revision,platform_days,upstream_days FROM control.retention_settings WHERE singleton"
	if lock {
		sql += " FOR UPDATE"
	}
	if tx.QueryRow(ctx, sql).Scan(&s.Revision, &s.PlatformDays, &s.UpstreamDays) != nil || !s.Valid() {
		return s, ErrUnavailable
	}
	return s, nil
}
func (r Repository) Retention(ctx context.Context) (Retention, error) {
	var s Retention
	e := r.transaction(ctx, true, func(ctx context.Context, tx storage.Transaction) error {
		var e error
		s, e = retention(ctx, tx, false)
		return e
	})
	return s, e
}
func (r Repository) SaveRetention(ctx context.Context, s Retention, actor string, now time.Time) (Retention, error) {
	if !s.Valid() {
		return s, ErrArgument
	}
	e := r.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		old, e := retention(ctx, tx, true)
		if e != nil {
			return e
		}
		if s.Revision != old.Revision {
			return ErrConflict
		}
		s.Revision++
		if _, e = tx.Exec(ctx, "UPDATE control.retention_settings SET revision=$1,platform_days=$2,upstream_days=$3 WHERE singleton", s.Revision, s.PlatformDays, s.UpstreamDays); e != nil {
			return ErrUnavailable
		}
		if _, e = tx.Exec(ctx, "INSERT INTO control.management_audit(id,subject,action,occurred_at) VALUES($1,$2,'retention_settings',$3)", NewID(), actor, now); e != nil {
			return ErrUnavailable
		}
		return nil
	})
	return s, e
}

type CleanupPreview struct {
	ID         string            `json:"id"`
	Subject    string            `json:"-"`
	Source     storage.LogSource `json:"source"`
	Revision   int64             `json:"revision,string"`
	Epoch      string            `json:"epoch"`
	Floor      int64             `json:"floor,string"`
	Through    int64             `json:"through,string"`
	Count      int64             `json:"count,string"`
	Cutoff     time.Time         `json:"cutoff"`
	ExpiresAt  time.Time         `json:"expires_at"`
	Protection string            `json:"protection"`
}
type Maintenance struct {
	Repository  Repository
	LogDatabase storage.Database
	Owner       string
}

type LogStorage struct {
	Epoch        string `json:"epoch"`
	HighWater    int64  `json:"high_water,string"`
	Floor        int64  `json:"floor,string"`
	EventBytes   int64  `json:"event_bytes,string"`
	ReceiptBytes int64  `json:"receipt_bytes,string"`
}

func (m Maintenance) LogStorage(ctx context.Context, source storage.LogSource) (LogStorage, error) {
	var s LogStorage
	e := m.logTx(ctx, source, func(ctx context.Context, tx storage.Transaction) error {
		if tx.QueryRow(ctx, "SELECT epoch,high_water,floor,pg_total_relation_size('logs.events'),pg_total_relation_size('logs.receipts') FROM logs.commit_state WHERE singleton").Scan(&s.Epoch, &s.HighWater, &s.Floor, &s.EventBytes, &s.ReceiptBytes) != nil {
			return ErrUnavailable
		}
		return nil
	})
	return s, e
}

func (m Maintenance) logTx(ctx context.Context, source storage.LogSource, fn func(context.Context, storage.Transaction) error) error {
	if m.LogDatabase == nil {
		return ErrUnavailable
	}
	c, e := m.Repository.Registry.LogMaintenance(source)
	if e != nil {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, e := m.LogDatabase.Begin(ctx, c)
	if e != nil {
		return ErrUnavailable
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	if e = fn(ctx, tx); e != nil {
		return e
	}
	if tx.Commit(ctx) != nil {
		return ErrUnavailable
	}
	return nil
}
func (m Maintenance) Preview(ctx context.Context, source storage.LogSource, subject string, now time.Time) (CleanupPreview, error) {
	if source != storage.PlatformLog && source != storage.UpstreamLog {
		return CleanupPreview{}, ErrArgument
	}
	s, e := m.Repository.Retention(ctx)
	if e != nil {
		return CleanupPreview{}, e
	}
	days := s.PlatformDays
	if source == storage.UpstreamLog {
		days = s.UpstreamDays
	}
	p := CleanupPreview{ID: NewID(), Subject: subject, Source: source, Revision: s.Revision, Cutoff: now.Add(-time.Duration(days) * 24 * time.Hour), ExpiresAt: now.Add(10 * time.Minute), Protection: "control_linked_events_permanently_protected"}
	if p.ID == "" {
		return p, ErrUnavailable
	}
	e = m.logTx(ctx, source, func(ctx context.Context, tx storage.Transaction) error {
		if tx.QueryRow(ctx, "SELECT epoch,floor,through,count FROM logs.cleanup_preview($1)", p.Cutoff).Scan(&p.Epoch, &p.Floor, &p.Through, &p.Count) != nil {
			return ErrUnavailable
		}
		return nil
	})
	if e != nil {
		return CleanupPreview{}, e
	}
	e = m.Repository.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		_, e := tx.Exec(ctx, "INSERT INTO control.cleanup_previews(id,subject,source,revision,epoch,floor,through,count,cutoff,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", p.ID, p.Subject, string(p.Source), p.Revision, p.Epoch, p.Floor, p.Through, p.Count, p.Cutoff, p.ExpiresAt)
		if e != nil {
			return ErrUnavailable
		}
		return nil
	})
	return p, e
}
func loadPreview(ctx context.Context, tx storage.Transaction, id, subject string) (CleanupPreview, error) {
	var p CleanupPreview
	var exists bool
	if tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM control.cleanup_previews WHERE id=$1 AND subject=$2)", id, subject).Scan(&exists) != nil {
		return p, ErrUnavailable
	}
	if !exists {
		return p, ErrMissing
	}
	if tx.QueryRow(ctx, "SELECT id,subject,source,revision,epoch,floor,through,count,cutoff,expires_at FROM control.cleanup_previews WHERE id=$1", id).Scan(&p.ID, &p.Subject, &p.Source, &p.Revision, &p.Epoch, &p.Floor, &p.Through, &p.Count, &p.Cutoff, &p.ExpiresAt) != nil {
		return p, ErrUnavailable
	}
	return p, nil
}

type CleanupRequest struct {
	PreviewID string `json:"preview_id"`
	Revision  int64  `json:"revision,string"`
}

func (m Maintenance) Accept(ctx context.Context, subject, key string, request CleanupRequest, now time.Time) (Job, error) {
	if !ValidID(request.PreviewID) || request.Revision < 1 {
		return Job{}, ErrArgument
	}
	return m.Repository.Accept(ctx, subject, "cleanup", key, request, now, 5*time.Minute, func(ctx context.Context, tx storage.Transaction) error {
		p, e := loadPreview(ctx, tx, request.PreviewID, subject)
		if e != nil {
			return e
		}
		s, e := retention(ctx, tx, true)
		if e != nil {
			return e
		}
		if !now.Before(p.ExpiresAt) || p.Revision != request.Revision || s.Revision != p.Revision {
			return ErrConflict
		}
		return nil
	})
}
func (m Maintenance) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.Pump(ctx)
		}
	}
}
func (m Maintenance) Pump(ctx context.Context) error {
	j, e := m.Repository.Claim(ctx, "cleanup", m.Owner, time.Now().UTC())
	if e != nil || j == nil {
		return e
	}
	var req CleanupRequest
	if json.Unmarshal(j.Request, &req) != nil {
		return ErrUnavailable
	}
	var p CleanupPreview
	var removed *int64
	e = m.Repository.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(734219100)"); e != nil {
			return ErrUnavailable
		}
		var owns bool
		if tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM control.management_jobs WHERE id=$1 AND owner=$2 AND status='running' AND lease_until>clock_timestamp())", j.ID, m.Owner).Scan(&owns) != nil {
			return ErrUnavailable
		}
		if !owns {
			return ErrConflict
		}
		var e error
		p, e = loadPreview(ctx, tx, req.PreviewID, j.Subject)
		if e != nil {
			return e
		}
		// Recovery acknowledges an already committed deletion before applying
		// changed policy/preview checks to a potential new deletion.
		if e = m.logTx(ctx, p.Source, func(ctx context.Context, log storage.Transaction) error {
			if log.QueryRow(ctx, "SELECT (SELECT deleted FROM logs.cleanup_receipts WHERE job_id=$1 AND epoch=$2 AND through=$3)", j.ID, p.Epoch, p.Through).Scan(&removed) != nil {
				return ErrUnavailable
			}
			return nil
		}); e != nil {
			return e
		}
		if removed != nil {
			return nil
		}
		s, e := retention(ctx, tx, true)
		if e != nil {
			return e
		}
		if s.Revision != p.Revision || !time.Now().Before(p.ExpiresAt) || !time.Now().Before(j.Deadline) {
			return ErrConflict
		}
		return m.logTx(ctx, p.Source, func(ctx context.Context, tx storage.Transaction) error {
			var count int64
			if tx.QueryRow(ctx, "SELECT logs.cleanup_prefix($1,$2,$3,$4,$5)", j.ID, p.Epoch, p.Floor, p.Through, p.Cutoff).Scan(&count) != nil {
				return ErrUnavailable
			}
			removed = &count
			return nil
		})
	})
	status := "succeeded"
	if e != nil {
		status = "partial_failed"
		removed = nil
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return m.Repository.Finish(finish, *j, status, map[string]any{"source": p.Source, "deleted": removed, "outcome_known": e == nil, "receipt_id": j.ID}, time.Now().UTC())
}
