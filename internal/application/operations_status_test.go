package application

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/runtimecontrol"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"strings"
	"testing"
	"time"
)

type statusRedis struct{ assembly.Redis }

func (statusRedis) Do(context.Context, string, ...any) (any, error) {
	return nil, errors.New("postgres://private:secret@internal")
}

type statusJournal struct {
	observability.Journal
	mode string
}

func (j statusJournal) Usage(ctx context.Context) (observability.Usage, error) {
	if j.mode == "timeout" {
		<-ctx.Done()
		return observability.Usage{}, ctx.Err()
	}
	if j.mode == "failed" {
		return observability.Usage{}, errors.New("private-directory")
	}
	return observability.Usage{Bytes: 123, Files: 2}, nil
}

type statusDB struct {
	storage.Database
	fail bool
}

func (d statusDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	if d.fail {
		return nil, errors.New("private-dsn")
	}
	return statusTx{}, nil
}

type statusTx struct{ storage.Transaction }

func (statusTx) Commit(context.Context) error                         { return nil }
func (statusTx) Rollback(context.Context) error                       { return nil }
func (statusTx) QueryRow(context.Context, string, ...any) storage.Row { return statusRow{} }

type statusRow struct{}

func (statusRow) Scan(v ...any) error {
	*v[0].(*string) = "epoch"
	*v[1].(*int64) = 10
	*v[2].(*int64) = 2
	*v[3].(*int64) = 4096
	*v[4].(*int64) = 8192
	return nil
}

func TestT34_01And02RealStatusProbeUnknownUnavailableAndRedaction(t *testing.T) {
	values := testkit.RuntimeValues("/private-directory")
	values["LOG_STORAGE_MODE"] = "dual"
	values["DATABASE_UPSTREAM_LOG_URL"] = "postgres://upstream:test@db-log:5432/upstream?sslmode=disable"
	values["UPSTREAM_LOG_DIRECTORY"] = "/private-directory/upstream"
	values["UPSTREAM_LOG_MAX_BYTES"] = "1048576"
	values["UPSTREAM_LOG_MAX_FILES"] = "100"
	cfg, err := config.LoadRuntime(config.Worker, func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"available", "failed", "timeout", "absent"} {
		t.Run(mode, func(t *testing.T) {
			rt := &assembly.Runtime{Redis: statusRedis{}, Files: &assembly.FileSet{}}
			if mode != "absent" {
				rt.Files.Journal = statusJournal{mode: mode}
				rt.Files.UpstreamJournal = statusJournal{mode: mode}
			}
			a := &Application{runtime: rt, config: cfg, routes: runtimecontrol.Routes{ActiveReader: publication.Atomic{Client: statusRedis{}}}, Maintenance: &operations.Maintenance{Repository: operations.Repository{Registry: cfg.Databases}, LogDatabase: statusDB{fail: mode == "failed"}}}
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			before := time.Now()
			got := a.OperationsStatus(ctx)
			if time.Since(before) > time.Second {
				t.Fatal("unbounded probe")
			}
			if got["observed_at"] == nil || got["smtp_reachability"] != "unknown" {
				t.Fatal(got)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"private-directory", "postgres://", "private-dsn", cfg.SnapshotKey, cfg.AdminPassword, cfg.SourceToken, cfg.MailPassword} {
				if secret != "" && strings.Contains(string(raw), secret) {
					t.Fatal("secret leaked", secret)
				}
			}
			if mode == "timeout" {
				if got["probe_state"] != "incomplete" && got["platform"].(map[string]any)["buffer_state"] != "unknown" {
					t.Fatal(got)
				}
				return
			}
			platform := got["platform"].(map[string]any)
			if mode == "available" {
				if platform["buffer_bytes"] != int64(123) || platform["buffer_files"] != int64(2) {
					t.Fatal(platform)
				}
			} else if platform["buffer_bytes"] != nil || platform["buffer_files"] != nil {
				t.Fatal("unknown reported zero", platform)
			}
			if mode == "failed" && (platform["storage"] != nil || platform["storage_state"] != "unavailable" || platform["buffer_state"] != "unavailable") {
				t.Fatal(platform)
			}
			credentials := got["credentials"].(map[string]bool)
			if credentials["smtp_configured"] != cfg.MailEnabled() || !credentials["log_cursor_configured"] {
				t.Fatal(credentials)
			}
		})
	}
}
