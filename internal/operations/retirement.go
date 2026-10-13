package operations

import (
	"context"
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"strings"
	"time"
)

func (m Maintenance) indexReferences(ctx context.Context) error {
	if m.Correlate == nil {
		return ErrUnavailable
	}
	namespace := m.Correlate("log-retirement-index-v1")
	return m.Repository.transaction(ctx, false, func(ctx context.Context, tx storage.Transaction) error {
		q, ok := tx.(storage.QueryExecutor)
		if !ok {
			return ErrUnavailable
		}
		rows, err := q.Query(ctx, `SELECT p.ref FROM control.log_identity_pending p WHERE NOT EXISTS(SELECT 1 FROM control.log_identity_map m WHERE m.namespace=$1 AND m.ref=p.ref) ORDER BY p.ref LIMIT 1000`, namespace)
		if err != nil {
			return ErrUnavailable
		}
		refs := []string{}
		for rows.Next() {
			var ref string
			if rows.Scan(&ref) != nil || len(refs) >= 1000 {
				rows.Close()
				return ErrUnavailable
			}
			refs = append(refs, ref)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrUnavailable
		}
		for _, ref := range refs {
			prefix, id, ok := strings.Cut(ref, ":")
			if !ok || id == "" {
				return ErrUnavailable
			}
			if _, err = tx.Exec(ctx, `INSERT INTO control.log_identity_map(namespace,ref,log_ref) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, namespace, ref, prefix+":"+m.Correlate(id)); err != nil {
				return ErrUnavailable
			}
		}
		return nil
	})
}

func (m Maintenance) eligibleLogReferences(ctx context.Context, tx storage.Transaction, refs []string) ([]string, error) {
	if m.Correlate == nil {
		return nil, ErrUnavailable
	}
	raw, _ := json.Marshal(refs)
	var out string
	if tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(m.log_ref ORDER BY m.log_ref),'[]'::jsonb)::text FROM control.log_identity_map m WHERE namespace=$1 AND $2::jsonb ? m.log_ref AND control.log_reference_eligible(m.ref)`, m.Correlate("log-retirement-index-v1"), string(raw)).Scan(&out) != nil {
		return nil, ErrUnavailable
	}
	return decodeReferences(out)
}

func (m Maintenance) rawReferences(ctx context.Context, tx storage.Transaction, refs []string) ([]string, error) {
	if m.Correlate == nil {
		return nil, ErrUnavailable
	}
	raw, _ := json.Marshal(refs)
	var out string
	if tx.QueryRow(ctx, `SELECT coalesce(jsonb_agg(m.ref ORDER BY m.ref),'[]'::jsonb)::text FROM control.log_identity_map m WHERE namespace=$1 AND $2::jsonb ? m.log_ref`, m.Correlate("log-retirement-index-v1"), string(raw)).Scan(&out) != nil {
		return nil, ErrUnavailable
	}
	return decodeReferences(out)
}

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
		rawRefs, err := m.rawReferences(ctx, tx, p.References)
		if err != nil {
			return err
		}
		if len(rawRefs) != len(p.References) {
			return ErrConflict
		}
		allowed, err := eligibleReferences(ctx, tx, rawRefs)
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
