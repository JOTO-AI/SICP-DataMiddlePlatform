package control

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// BundleChecks reads all successful dataset checks in one database statement,
// so a concurrent complete-round update cannot produce a mixed metadata view.
// P12 writes these facts with confirmed publication of a completely checked
// bundle; P13 also refreshes them with complete unchanged rounds. Never on fetch
// start, partial completion, or an unconfirmed publication response.
func (r Repository) BundleChecks(ctx context.Context, bundle string) (map[string]time.Time, error) {
	if strings.TrimSpace(bundle) == "" || len(bundle) > 256 {
		return nil, ErrEntity
	}
	var encoded string
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(jsonb_object_agg(c.source,c.checked_at),'{}'::jsonb)::text FROM control.bundle_checks c JOIN control.bundles b ON b.id=c.bundle_id WHERE c.bundle_id=$1 AND b.published_at IS NOT NULL`, bundle).Scan(&encoded)
	})
	if err != nil {
		return nil, err
	}
	var checks map[string]time.Time
	if err = json.Unmarshal([]byte(encoded), &checks); err != nil {
		return nil, err
	}
	return checks, nil
}
