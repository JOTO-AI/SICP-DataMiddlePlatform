package adapters

import (
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT28_03LegacyAdapterRejectsDualBeforeOpeningPools(t *testing.T) {
	v := testkit.RuntimeValues("/data")
	v["LOG_STORAGE_MODE"] = "dual"
	v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://upstream:synthetic@db-upstream:5432/logs?sslmode=disable"
	r, err := storage.LoadRegistry(func(k string) (string, bool) { value, ok := v[k]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	// No external connection should be attempted for a valid but unassembled mode.
	db, err := OpenPostgres(t.Context(), r, time.Millisecond)
	if db != nil || err != ErrPostgres {
		t.Fatal("legacy adapter ignored upstream target", err)
	}
}
