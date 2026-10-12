package config_test

import (
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT28_03RuntimeRejectsUnassembledDualPipeline(t *testing.T) {
	for _, role := range []config.Role{config.Query, config.Worker} {
		v := testkit.RuntimeValues("/data")
		lookup := func(k string) (string, bool) { value, ok := v[k]; return value, ok }
		c, err := config.LoadRuntime(role, lookup)
		if err != nil || c.Databases.LogMode() != storage.SingleLegacy {
			t.Fatal("explicit compatibility rejected", err)
		}
		v["LOG_STORAGE_MODE"] = "dual"
		v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://upstream:synthetic-upstream-secret@upstream:5432/logs?sslmode=disable"
		if _, err := config.LoadRuntime(role, lookup); err == nil || !strings.Contains(err.Error(), "not assembled") || strings.Contains(err.Error(), "synthetic-upstream-secret") {
			t.Fatal("runtime falsely enabled dual pipeline", err)
		}
	}
}
