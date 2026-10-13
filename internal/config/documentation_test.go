package config_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func docRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestT35_05UpgradeCommandsVersionsAndConfigurationGates(t *testing.T) {
	doc := string(docRead(t, "development_docs/OPERATIONS_UPGRADE_RUNBOOK.md"))
	script := string(docRead(t, "scripts/sicp.sh"))
	commands := regexp.MustCompile(`(?m)^scripts/sicp\.sh ([a-z-]+)$`).FindAllStringSubmatch(doc, -1)
	if len(commands) < 8 {
		t.Fatal("upgrade command sequence missing")
	}
	for _, match := range commands {
		if !regexp.MustCompile(`(?m)^  [a-z|-]*\b` + regexp.QuoteMeta(match[1]) + `[a-z|-]*\)`).MatchString(script) {
			t.Fatal("nonexistent documented command", match[1])
		}
	}
	for _, domain := range []migrate.Domain{migrate.Business, migrate.Control, migrate.Logs, migrate.UpstreamLogs} {
		plan, err := migrate.Catalog(domain)
		if err != nil || len(plan) == 0 {
			t.Fatal(err)
		}
		if !strings.Contains(doc, "`"+plan[len(plan)-1].Version+"`") {
			t.Fatal("stale upgrade migration version", domain)
		}
	}
	for _, role := range []config.Role{config.Query, config.Worker} {
		values := testkit.RuntimeValues("/data")
		values["LOG_STORAGE_MODE"] = "dual"
		values["DATABASE_UPSTREAM_LOG_URL"] = "postgres://upstream:synthetic-password@db-log:5432/upstream_logs?sslmode=disable"
		values["UPSTREAM_LOG_DIRECTORY"] = "/data/upstream-logs"
		values["UPSTREAM_LOG_MAX_BYTES"] = "1048576"
		values["UPSTREAM_LOG_MAX_FILES"] = "1000"
		lookup := func(k string) (string, bool) { v, ok := values[k]; return v, ok }
		if _, err := config.LoadRuntime(role, lookup); err != nil {
			t.Fatal("documented dual baseline", err)
		}
		for _, key := range []string{"DATABASE_UPSTREAM_LOG_URL", "SNAPSHOT_KEY", "AUDIT_KEY"} {
			old := values[key]
			delete(values, key)
			if _, err := config.LoadRuntime(role, lookup); err == nil {
				t.Fatal("missing deployment prerequisite admitted", key)
			}
			values[key] = old
		}
	}
	for _, requirement := range []string{"旧 writer", "停写维护", "恢复点", "epoch = gen_random_uuid()::text", "旧 Sender", "receipts", "禁止", "未执行升级"} {
		if !strings.Contains(doc, requirement) {
			t.Fatal("rollback requirement missing", requirement)
		}
	}
}
