package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func TestT32_13DuplicateAcceptancePrecedesChangedPreconditions(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	request := TestRequest{APIID: "dwd_bzkscjzlb", Version: CatalogVersion, Page: 1, PageSize: 10}
	raw, _ := json.Marshal(request)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	id := "existing-job"
	for _, conflict := range []bool{false, true} {
		tx := &fakeTx{row: func(q string, args []any) storage.Row {
			return fakeRow(func(d ...any) error {
				if strings.Contains(q, "request_hash FROM") {
					*d[0].(**string) = &id
					hash := digest
					if conflict {
						hash = "different"
					}
					*d[1].(**string) = &hash
					return nil
				}
				if len(d) != 13 {
					return errors.New("unexpected query")
				}
				*d[0].(*string) = id
				*d[1].(*string) = "alice"
				*d[2].(*string) = "source_test"
				*d[3].(*string) = string(raw)
				*d[4].(*string) = "succeeded"
				*d[5].(*time.Time) = now
				*d[6].(*time.Time) = now.Add(30 * time.Second)
				*d[7].(*time.Time) = now.Add(7 * 24 * time.Hour)
				*d[12].(*string) = `{}`
				return nil
			})
		}, exec: func(q string, args []any) (int64, error) {
			if strings.Contains(q, "INSERT") || strings.Contains(q, "UPDATE") {
				t.Fatal("duplicate wrote a new job")
			}
			return 1, nil
		}}
		j, e := fixtureRepo(tx).Accept(t.Context(), "alice", "source_test", "same-key", request, now, time.Second, func(context.Context, storage.Transaction) error {
			t.Fatal("duplicate rechecked changed preconditions")
			return ErrConflict
		})
		if conflict {
			if e != ErrConflict || tx.committed {
				t.Fatal("different request accepted")
			}
		} else if e != nil || j.ID != id || !tx.committed {
			t.Fatal(e)
		}
	}
}

func TestT32_13AcceptanceAndAuditAreAtomicAndQueriesBounded(t *testing.T) {
	tx := &fakeTx{row: func(string, []any) storage.Row {
		return fakeRow(func(d ...any) error { *d[0].(**string) = nil; *d[1].(**string) = nil; return nil })
	}, exec: func(q string, args []any) (int64, error) {
		if strings.Contains(q, "management_audit") {
			return 0, errors.New("synthetic audit failure")
		}
		return 1, nil
	}}
	j, e := fixtureRepo(tx).Accept(t.Context(), "alice", "cleanup", "request-1", map[string]any{}, time.Now(), time.Minute, func(ctx context.Context, _ storage.Transaction) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("unbounded admission check")
		}
		return nil
	})
	if e != ErrUnavailable || j.ID != "" || tx.committed || !tx.rolled {
		t.Fatal("acceptance escaped failed audit transaction")
	}
}

func TestT32_13LateExecutorCannotOverwriteJob(t *testing.T) {
	tx := &fakeTx{exec: func(q string, a []any) (int64, error) {
		if strings.Contains(q, "UPDATE control.management_jobs") {
			if !strings.Contains(q, "owner=$2") || !strings.Contains(q, "lease_until>$5") {
				t.Fatal("missing ownership fence")
			}
			return 0, nil
		}
		return 1, nil
	}}
	owner := "old-owner"
	if e := fixtureRepo(tx).Finish(t.Context(), Job{ID: "job", Owner: &owner}, "succeeded", map[string]any{}, time.Now()); e != ErrConflict || tx.committed {
		t.Fatal("late executor was accepted")
	}
}
