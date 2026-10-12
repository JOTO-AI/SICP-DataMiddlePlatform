package adapters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestT28_11IndependentLogPoolsAreLazyAndDoNotBlockStartup(t *testing.T) {
	v := testkit.RuntimeValues("/data")
	v["LOG_STORAGE_MODE"] = "dual"
	v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://upstream:synthetic@db-upstream:5432/logs?sslmode=disable"
	r, err := storage.LoadRegistry(func(k string) (string, bool) { value, ok := v[k]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	// NewWithConfig creates lazy pools. Substitute only Ping, never dial a server.
	var probed []string
	db, err := openPostgresWithLimits(t.Context(), r, time.Millisecond, PostgresPoolLimits{8, 4, 2}, func(_ context.Context, p *pgxpool.Pool) error {
		probed = append(probed, p.Config().ConnConfig.Host)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(t.Context())
	if len(db.pools) != 7 || len(probed) != 5 {
		t.Fatal("logs are eager or pool missing")
	}
	u, _ := r.LogConnection(storage.UpstreamLog)
	if db.pools[u] == db.pools[r.Logs()] || db.pools[u].Config().MaxConns != 2 || db.pools[r.Logs()].Config().MaxConns != 2 {
		t.Fatal("log pool limits coupled")
	}
	for _, host := range probed {
		if host == "db-upstream" || host == "db-upstream-log" || host == "db-log" {
			t.Fatal("log health gates startup")
		}
	}
	partial, err := openPostgresWithLimits(t.Context(), r, time.Millisecond, PostgresPoolLimits{8, 4, 2}, func(context.Context, *pgxpool.Pool) error { return errors.New("private-driver-error") })
	if err != ErrPostgres || partial == nil || len(partial.order) != 1 {
		t.Fatal("fatal partial construction hidden")
	}
	if partial.Close(t.Context()) != nil {
		t.Fatal("partial cleanup failed")
	}
}
