package operations

import (
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"strings"
	"testing"
	"time"
)

func TestT34_03RetentionRevisionAuditAndCommitFailures(t *testing.T) {
	for _, mode := range []string{"saved", "stale", "audit_failed", "update_failed"} {
		t.Run(mode, func(t *testing.T) {
			audit, updated := false, false
			tx := &fakeTx{row: func(q string, a []any) storage.Row {
				return fakeRow(func(v ...any) error {
					if !strings.HasSuffix(q, "FOR UPDATE") {
						t.Fatal("unlocked settings")
					}
					*v[0].(*int64) = 4
					*v[1].(*int) = 30
					*v[2].(*int) = 30
					return nil
				})
			}, exec: func(q string, a []any) (int64, error) {
				if strings.HasPrefix(q, "UPDATE") {
					updated = true
					if mode == "update_failed" {
						return 0, errors.New("private")
					}
				}
				if strings.Contains(q, "management_audit") {
					audit = true
					if a[1] != "trusted-admin" {
						t.Fatal("wrong actor")
					}
					if mode == "audit_failed" {
						return 0, errors.New("private")
					}
				}
				return 1, nil
			}}
			revision := int64(4)
			if mode == "stale" {
				revision = 3
			}
			got, err := fixtureRepo(tx).SaveRetention(t.Context(), Retention{revision, 90, 7}, "trusted-admin", time.Now())
			if mode == "saved" {
				if err != nil || got.Revision != 5 || !tx.committed || !audit || !updated {
					t.Fatal(got, err)
				}
			} else {
				if err == nil || tx.committed || got.Revision != 0 {
					t.Fatal("failed save returned applied revision", got, err)
				}
			}
		})
	}
}
