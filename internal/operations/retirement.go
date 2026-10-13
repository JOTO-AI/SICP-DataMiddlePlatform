package operations

import (
	"context"
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"strings"
	"time"
)

// Only the bounded log prefix supplies identities; callers cannot submit paths,
// SQL, table names or reference lists through the management API.
func decodeReferences(raw string) ([]string, error) {
	var refs []string
	if len(raw) > 160000 || json.Unmarshal([]byte(raw), &refs) != nil || len(refs) > 1000 {
		return nil, ErrUnavailable
	}
	if refs == nil {
		refs = []string{}
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		prefix, id, ok := strings.Cut(ref, ":")
		if !ok || (prefix != "task" && prefix != "test" && prefix != "audit") || !ValidID(id) || seen[ref] {
			return nil, ErrUnavailable
		}
		seen[ref] = true
	}
	return refs, nil
}

func eligibleReferences(ctx context.Context, tx storage.Transaction, refs []string) ([]string, error) {
	raw, _ := json.Marshal(refs)
	var allowed string
	if tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(ref ORDER BY ref),'[]'::jsonb)::text FROM jsonb_array_elements_text($1::jsonb) ref WHERE control.log_reference_eligible(ref)`, string(raw)).Scan(&allowed) != nil {
		return nil, ErrUnavailable
	}
	return decodeReferences(allowed)
}

// This commit precedes log deletion. A crash between databases can leave an
// unused retirement fence; it cannot delete before new dependencies are fenced.
func (m Maintenance) retireReferences(ctx context.Context, j Job, req CleanupRequest) error {
	return m.Repository.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(734219100)"); err != nil {
			return ErrUnavailable
		}
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(734219120)"); err != nil {
			return ErrUnavailable
		}
		var owns bool
		if tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM control.management_jobs WHERE id=$1 AND owner=$2 AND status='running' AND lease_until>clock_timestamp() AND deadline>clock_timestamp())", j.ID, m.Owner).Scan(&owns) != nil {
			return ErrUnavailable
		}
		if !owns {
			return ErrConflict
		}
		p, err := loadPreview(ctx, tx, req.PreviewID, j.Subject)
		if err != nil {
			return err
		}
		s, err := retention(ctx, tx, false)
		if err != nil {
			return err
		}
		if s.Revision != p.Revision || !time.Now().Before(p.ExpiresAt) {
			return ErrConflict
		}
		if len(p.References) == 0 {
			return nil
		}
		allowed, err := eligibleReferences(ctx, tx, p.References)
		if err != nil {
			return err
		}
		if len(allowed) != len(p.References) {
			return ErrConflict
		}
		raw, _ := json.Marshal(allowed)
		var retired bool
		if tx.QueryRow(ctx, "SELECT control.retire_log_references($1::jsonb)", string(raw)).Scan(&retired) != nil {
			return ErrUnavailable
		}
		if !retired {
			return ErrConflict
		}
		return nil
	})
}
