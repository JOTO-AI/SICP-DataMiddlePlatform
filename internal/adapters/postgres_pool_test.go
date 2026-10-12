package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func TestPostgresPoolConfigUsesExplicitLimit(t *testing.T) {
	for _, limit := range []int{1, 16, 32, 1000} {
		cfg, err := postgresPoolConfig("postgres://localhost/test?pool_max_conns=99&pool_min_conns=5&pool_min_idle_conns=5", 3*time.Second, limit, "query")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MaxConns != int32(limit) || cfg.MinConns != 0 || cfg.MinIdleConns != 0 || cfg.ConnConfig.ConnectTimeout != 3*time.Second ||
			cfg.ConnConfig.RuntimeParams["timezone"] != "UTC" || cfg.ConnConfig.RuntimeParams["application_name"] != "sicp-query" || cfg.AfterConnect == nil {
			t.Fatal("pool configuration not applied")
		}
	}
	for _, limit := range []int{-1, 0, 1001} {
		if _, err := postgresPoolConfig("postgres://localhost/test", time.Second, limit, ""); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
}

func TestPostgresRejectsInvalidLimitsBeforeConnecting(t *testing.T) {
	for _, limits := range []PostgresPoolLimits{{0, 16, 16}, {16, -1, 16}, {16, 16, 1001}} {
		if _, err := OpenPostgresWithLimits(context.Background(), storage.Registry{}, time.Second, limits); err != ErrPostgres {
			t.Fatal("invalid limits reached database", err)
		}
	}
}
