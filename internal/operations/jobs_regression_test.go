package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func jobRow(j Job) storage.Row {
	return fakeRow(func(d ...any) error {
		*d[0].(*string), *d[1].(*string), *d[2].(*string), *d[3].(*string) = j.ID, j.Subject, j.Action, string(j.Request)
		*d[4].(*string), *d[5].(*time.Time), *d[6].(*time.Time), *d[7].(*time.Time) = j.Status, j.AcceptedAt, j.Deadline, j.ExpiresAt
		*d[8].(**string), *d[9].(**time.Time), *d[10].(**time.Time), *d[11].(**time.Time), *d[12].(*string) = j.Owner, j.LeaseUntil, j.StartedAt, j.FinishedAt, string(j.Summary)
		return nil
	})
}

func TestT32_13IdempotentReplayPrecedesBusyAndQueueChecks(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	request := TestRequest{APIID: "synthetic", Version: CatalogVersion, Page: 1, PageSize: 10}
	raw, _ := json.Marshal(request)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	for _, state := range []string{"queued", "running", "succeeded"} {
		t.Run(state, func(t *testing.T) {
			j := Job{ID: "same-job", Subject: "actor", Action: "source_test", Status: state, Request: raw, Summary: json.RawMessage(`{}`), ExpiresAt: now.Add(time.Hour)}
			if state != "succeeded" {
				j.ExpiresAt = now.Add(-time.Hour)
			}
			tx := &fakeTx{row: func(q string, args []any) storage.Row {
				if strings.Contains(q, "request_hash") {
					return fakeRow(func(d ...any) error { *d[0].(**string), *d[1].(**string) = &j.ID, &digest; return nil })
				}
				return jobRow(j)
			}, exec: func(q string, args []any) (int64, error) {
				if !strings.Contains(q, "pg_advisory_xact_lock") {
					t.Fatal("replay mutated existing job", q)
				}
				return 1, nil
			}}
			got, err := fixtureRepo(tx).Accept(t.Context(), "actor", "source_test", "same-key", request, now, 30*time.Second, func(context.Context, storage.Transaction) error {
				t.Fatal("replay rechecked admission")
				return ErrLimit
			})
			if err != nil || got.ID != j.ID {
				t.Fatal(got, err)
			}
			request2 := request
			request2.Page = 2
			if _, err = fixtureRepo(tx).Accept(t.Context(), "actor", "source_test", "same-key", request2, now, 30*time.Second, nil); err != ErrConflict {
				t.Fatal("payload conflict accepted", err)
			}
		})
	}
}

func TestT32_12QueueLimitHasNoAcceptanceOrAudit(t *testing.T) {
	tx := &fakeTx{row: func(q string, args []any) storage.Row {
		return fakeRow(func(d ...any) error {
			if strings.Contains(q, "request_hash") {
				*d[0].(**string), *d[1].(**string) = nil, nil
			} else {
				*d[0].(*int) = 20
			}
			return nil
		})
	}, exec: func(q string, args []any) (int64, error) {
		if strings.Contains(q, "INSERT") {
			t.Fatal("full queue accepted work")
		}
		return 1, nil
	}}
	if _, err := fixtureRepo(tx).Accept(t.Context(), "actor", "source_test", "key", map[string]any{}, time.Now(), 30*time.Second, nil); err != ErrLimit || tx.committed {
		t.Fatal(err)
	}
}

func TestT32_13ClaimRecoveryAndLateCompletion(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	job := Job{ID: "queued-job", Subject: "actor", Action: "source_test", Status: "queued", AcceptedAt: now.Add(-20 * time.Second), Deadline: now.Add(10 * time.Second), Request: json.RawMessage(`{}`), Summary: json.RawMessage(`{}`)}
	sawRecovery := false
	tx := &fakeTx{row: func(q string, args []any) storage.Row {
		if strings.Contains(q, "SELECT (SELECT id") {
			return fakeRow(func(d ...any) error { *d[0].(**string) = &job.ID; return nil })
		}
		return jobRow(job)
	}, exec: func(q string, args []any) (int64, error) {
		if strings.Contains(q, "deadline_or_executor_lost") {
			sawRecovery = strings.Contains(q, "'interrupted'") && strings.Contains(q, "lease_until<=$1")
			if args[1] != "source_test" {
				t.Fatal("wrong recovery scope")
			}
		}
		if strings.Contains(q, "status='running',owner") && !args[2].(time.Time).Equal(job.Deadline.Add(5*time.Second)) {
			t.Fatal("claim extended acceptance deadline")
		}
		return 1, nil
	}}
	j, err := fixtureRepo(tx).Claim(t.Context(), "source_test", "new-worker", now)
	if err != nil || j == nil || !sawRecovery || !j.Deadline.Equal(job.Deadline) {
		t.Fatal(j, err)
	}
	tx.exec = func(q string, args []any) (int64, error) {
		if !strings.Contains(q, "owner=$2") || !strings.Contains(q, "lease_until>$5") || !strings.Contains(q, "status='running'") {
			t.Fatal("late completion not fenced")
		}
		return 0, nil
	}
	if err = fixtureRepo(tx).Finish(t.Context(), *j, "succeeded", map[string]any{}, now); err != ErrConflict {
		t.Fatal("lost executor completed", err)
	}
}

func TestT32_14ResultsExpiryIsolationEvictionAndCopies(t *testing.T) {
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	r := NewResults()
	raw := []byte(`{"synthetic":"value"}`)
	if !r.Put("job", "actor", raw, now) {
		t.Fatal("put")
	}
	raw[2] = 'X'
	got, ok := r.Get("job", "actor", now)
	if !ok || string(got) != `{"synthetic":"value"}` {
		t.Fatal("caller mutated result")
	}
	got[2] = 'Y'
	if _, ok = r.Get("job", "other", now); ok {
		t.Fatal("cross subject access")
	}
	if _, ok = NewResults().Get("job", "actor", now); ok {
		t.Fatal("results survived executor restart")
	}
	if _, ok = r.Get("job", "actor", now.Add(10*time.Minute)); ok || r.bytes != 0 {
		t.Fatal("expired result retained")
	}
	large := []byte(`"` + strings.Repeat("x", 2<<20-2) + `"`)
	for i := 0; i < 11; i++ {
		if !r.Put(fmt.Sprintf("large-%02d", i), "actor", large, now.Add(time.Duration(i)*time.Second)) {
			t.Fatal("bounded insertion failed")
		}
	}
	if _, ok = r.Get("large-00", "actor", now.Add(time.Minute)); ok || r.bytes > 20<<20 {
		t.Fatal("byte eviction failed")
	}
	r = NewResults()
	for i := 0; i < 1001; i++ {
		if !r.Put(fmt.Sprintf("tiny-%04d", i), "actor", []byte(`{}`), now.Add(time.Duration(i)*time.Millisecond)) {
			t.Fatal("tiny insertion")
		}
	}
	if _, ok = r.Get("tiny-0000", "actor", now.Add(time.Minute)); ok || len(r.items) > 1000 {
		t.Fatal("metadata unbounded")
	}
}
