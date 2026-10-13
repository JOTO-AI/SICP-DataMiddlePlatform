package migrate_test

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"strings"
	"testing"
)

func TestT29_01CommitCounterReceiptAndWriterProtocol(t *testing.T) {
	for _, domain := range []migrate.Domain{migrate.Logs, migrate.UpstreamLogs} {
		plan, err := migrate.Catalog(domain)
		if err != nil {
			t.Fatal(err)
		}
		sql := commitProtocol(t, domain, plan)
		for _, required := range []string{"FOR UPDATE", "high_water=high_water+1", "INSERT INTO logs.receipts", "sha256(convert_to(payload::text,'UTF8'))", "SECURITY DEFINER SET search_path=pg_catalog", "REVOKE INSERT,UPDATE,DELETE,TRUNCATE", "RETURN -1"} {
			if !strings.Contains(sql, required) {
				t.Fatal("missing protocol", domain, required)
			}
		}
		if strings.Index(sql, "FOR UPDATE") > strings.Index(sql, "IF FOUND THEN") || strings.Index(sql, "IF FOUND THEN") > strings.Index(sql, "high_water=high_water+1") {
			t.Fatal("allocation before deduplication/lock")
		}
		if strings.Contains(sql, "BIGSERIAL") || strings.Contains(sql, "nextval(") {
			t.Fatal("nontransactional high water")
		}
	}
}
func TestT29_04BackfillAndNoLegacyWriterBypass(t *testing.T) {
	for _, domain := range []migrate.Domain{migrate.Logs, migrate.UpstreamLogs} {
		plan, _ := migrate.Catalog(domain)
		sql := commitProtocol(t, domain, plan)
		for _, required := range []string{"LOCK TABLE logs.events IN ACCESS EXCLUSIVE MODE", "row_number() OVER (ORDER BY received_at,event_id)", "ALTER COLUMN sequence SET NOT NULL", "CREATE UNIQUE INDEX events_sequence", "gen_random_uuid()::text", "REVOKE ALL ON FUNCTION logs.append_event"} {
			if !strings.Contains(sql, required) {
				t.Fatal("missing upgrade boundary", required)
			}
		}
	}
}

func commitProtocol(t *testing.T, domain migrate.Domain, plan []migrate.Migration) string {
	t.Helper()
	version := "logs_005"
	if domain == migrate.UpstreamLogs {
		version = "upstream_logs_003"
	}
	for _, migration := range plan {
		if migration.Version == version {
			return migration.SQL
		}
	}
	t.Fatal("writer protocol migration missing", version)
	return ""
}
