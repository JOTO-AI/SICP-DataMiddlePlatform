package config_test

import (
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT28_10RuntimeRequiresIndependentDualCapacity(t *testing.T) {
	for _, role := range []config.Role{config.Query, config.Worker} {
		v := testkit.RuntimeValues("/data")
		lookup := func(k string) (string, bool) { value, ok := v[k]; return value, ok }
		c, err := config.LoadRuntime(role, lookup)
		if err != nil || c.Databases.LogMode() != storage.SingleLegacy {
			t.Fatal("explicit compatibility rejected", err)
		}
		v["LOG_STORAGE_MODE"] = "dual"
		v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://upstream:synthetic-upstream-secret@upstream:5432/logs?sslmode=disable"
		if _, err := config.LoadRuntime(role, lookup); err == nil || strings.Contains(err.Error(), "synthetic-upstream-secret") {
			t.Fatal("runtime accepted missing upstream capacity", err)
		}
		v["UPSTREAM_LOG_DIRECTORY"] = "/data/upstream-logs"
		v["UPSTREAM_LOG_MAX_BYTES"] = "1048576"
		v["UPSTREAM_LOG_MAX_FILES"] = "1000"
		if _, err := config.LoadRuntime(role, lookup); err != nil {
			t.Fatal(err)
		}
		v["UPSTREAM_LOG_DIRECTORY"] = v["LOG_DIRECTORY"]
		if _, err := config.LoadRuntime(role, lookup); err == nil {
			t.Fatal("overlapping source directories accepted")
		}
	}
}
