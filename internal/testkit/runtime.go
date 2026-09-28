package testkit

import (
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"path"
)

// RuntimeValues contains only synthetic identities and explicit local fixture
// endpoints. Callers supply an isolated persistent root and override endpoints
// with the particular mock listeners they own.
func RuntimeValues(root string) map[string]string {
	quality := map[string]merge.Quality{}
	for _, t := range source.Catalog() {
		quality[t.Name] = merge.Quality{MaximumDeletedRows: 1000}
	}
	q, _ := json.Marshal(quality)
	return map[string]string{
		"SOURCE_SCOPE": "synthetic-sicp-source", "SOURCE_QUALITY": string(q),
		"QUERY_API_KEY":        "synthetic-query-secret",
		"DATABASE_M0_URL":      "postgres://worker:synthetic@db-m:5432/slot0?sslmode=disable",
		"DATABASE_M1_URL":      "postgres://worker:synthetic@db-m:5432/slot1?sslmode=disable",
		"DATABASE_S0_URL":      "postgres://worker:synthetic@db-s:5432/slot0?sslmode=disable",
		"DATABASE_S1_URL":      "postgres://worker:synthetic@db-s:5432/slot1?sslmode=disable",
		"DATABASE_CONTROL_URL": "postgres://worker:synthetic@db-m:5432/control?sslmode=disable",
		"DATABASE_LOG_URL":     "postgres://logger:synthetic@db-log:5432/logs?sslmode=disable",
		"REDIS_URL":            "redis://redis:6379/0",
		"SOURCE_ORIGIN":        "http://mock-source:8090", "SOURCE_TOKEN": "synthetic-source-token",
		"MAIL_ADDRESS": "mock-mail:1025", "MAIL_TLS": "none", "MAIL_FROM": "worker@example.invalid", "MAIL_TO": "operator@example.invalid",
		"LOG_DIRECTORY": path.Join(root, "logs"), "STAGING_DIRECTORY": path.Join(root, "staging"), "TARGET_DIRECTORY": path.Join(root, "targets"), "SPOOL_DIRECTORY": path.Join(root, "spool"),
		"LOG_MAX_BYTES": "16777216", "LOG_MAX_FILES": "10000", "STAGING_MAX_BYTES": "67108864", "TARGET_MAX_BYTES": "67108864", "SPOOL_MAX_BYTES": "33554432",
		"SNAPSHOT_KEY": "synthetic-snapshot-key-0000000000", "AUDIT_KEY": "synthetic-audit-key-0000000000000", "ENVIRONMENT": "test",
		"ADMIN_USERNAME": "synthetic-admin", "ADMIN_PASSWORD": "synthetic-admin-password", "CONSOLE_QUERY_URL": "http://query:8080", "CONSOLE_COOKIE_SECURE": "false",
	}
}
