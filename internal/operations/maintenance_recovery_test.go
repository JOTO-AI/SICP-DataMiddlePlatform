package operations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func maintenanceRegistry(t *testing.T) storage.Registry {
	t.Helper()
	v := map[string]string{"LOG_STORAGE_MODE": "dual", "DATABASE_M0_URL": "postgres://worker:test@db-m:5432/m0?sslmode=disable", "DATABASE_M1_URL": "postgres://worker:test@db-m:5432/m1?sslmode=disable", "DATABASE_S0_URL": "postgres://worker:test@db-s:5432/s0?sslmode=disable", "DATABASE_S1_URL": "postgres://worker:test@db-s:5432/s1?sslmode=disable", "DATABASE_CONTROL_URL": "postgres://worker:test@db-m:5432/control?sslmode=disable", "DATABASE_LOG_URL": "postgres://logger:test@db-log:5432/logs?sslmode=disable", "DATABASE_UPSTREAM_LOG_URL": "postgres://upstream:test@db-log:5432/upstream?sslmode=disable"}
	r, err := storage.LoadRegistry(func(k string) (string, bool) { s, ok := v[k]; return s, ok })
	if err != nil {
		t.Fatal(err)
	}
	r, err = r.WithServiceLogAccess(true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestT34_06And12AmbiguousCleanupRecoversReceiptBeforePolicy(t *testing.T) {
	for _, mode := range []string{"ambiguous", "recovered", "expired"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			final := false
			deletions := 0
			policyRead := false
			resultRead := false
			job := Job{ID: "cleanup-job", Subject: "actor", Action: "cleanup", Status: "queued", AcceptedAt: now.Add(-time.Hour), Deadline: now.Add(-time.Minute), Request: json.RawMessage(`{"preview_id":"preview","revision":"1"}`), Summary: json.RawMessage(`{}`)}
			tx := &fakeTx{row: func(q string, a []any) storage.Row {
				switch {
				case strings.Contains(q, "SELECT (SELECT id"):
					return fakeRow(func(d ...any) error { *d[0].(**string) = &job.ID; return nil })
				case strings.HasPrefix(q, "SELECT EXISTS"):
					return fakeRow(func(d ...any) error { *d[0].(*bool) = true; return nil })
				case strings.Contains(q, "SELECT id,subject,source"):
					return fakeRow(func(d ...any) error {
						*d[0].(*string), *d[1].(*string), *d[2].(*storage.LogSource) = "preview", "actor", storage.PlatformLog
						*d[3].(*int64), *d[4].(*string), *d[5].(*int64), *d[6].(*int64), *d[7].(*int64) = 1, "epoch", 0, 7, 7
						*d[8].(*time.Time), *d[9].(*time.Time) = now.Add(-30*24*time.Hour), now.Add(-time.Minute)
						return nil
					})
				case strings.Contains(q, "control.retention_settings"):
					policyRead = true
					return fakeRow(func(d ...any) error { *d[0].(*int64), *d[1].(*int), *d[2].(*int) = 2, 30, 30; return nil })
				default:
					return jobRow(job)
				}
			}, exec: func(q string, a []any) (int64, error) {
				if strings.Contains(q, "status=$3,summary=") {
					final = true
					var s struct {
						Deleted *int64 `json:"deleted"`
						Known   bool   `json:"outcome_known"`
					}
					if json.Unmarshal([]byte(a[3].(string)), &s) != nil || !s.Known || s.Deleted == nil {
						t.Fatal("unknown presented as final")
					}
					want := int64(0)
					if mode == "recovered" {
						want = 7
					}
					if *s.Deleted != want {
						t.Fatal("incorrect actual deletion")
					}
				}
				return 1, nil
			}}
			log := &fakeTx{row: func(q string, a []any) storage.Row {
				return fakeRow(func(d ...any) error {
					if strings.Contains(q, "cleanup_prefix") {
						deletions++
					}
					if !strings.Contains(q, "cleanup_result") {
						t.Fatal("cleanup bypassed recovery receipt")
					}
					resultRead = true
					if mode == "ambiguous" {
						return errors.New("log database unavailable")
					}
					if mode == "recovered" {
						n := int64(7)
						*d[0].(**int64) = &n
					}
					return nil
				})
			}}
			m := Maintenance{Repository: Repository{Registry: maintenanceRegistry(t), Database: fakeDB{tx}}, LogDatabase: fakeDB{log}, Owner: "new-worker"}
			err := m.Pump(context.Background())
			if !resultRead || deletions != 0 {
				t.Fatal("recovery repeated deletion")
			}
			if mode == "ambiguous" {
				if err != ErrUnavailable || final {
					t.Fatal("ambiguous commit finalized", err)
				}
			} else if err != nil || !final {
				t.Fatal(err)
			}
			if policyRead != (mode == "expired") {
				t.Fatal("policy invalidated committed receipt")
			}
		})
	}
}
