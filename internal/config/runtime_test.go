package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT15_09RuntimeRejectsIncompleteOrConflictingDeployment(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"DATABASE_M1_URL", ""}, {"SOURCE_ORIGIN", ""}, {"SOURCE_ORIGIN", "http://mock.invalid/production-path"}, {"SOURCE_TOKEN", ""}, {"MAIL_ADDRESS", ""}, {"MAIL_TLS", ""}, {"MAIL_TO", "attacker\r\nBcc:someone"},
		{"SOURCE_SCOPE", ""}, {"SOURCE_QUALITY", ""}, {"SOURCE_QUALITY", "{}"}, {"SOURCE_QUALITY", "null"},
		{"REDIS_URL", "redis://redis:6379/0?read_timeout=0"}, {"REDIS_URL", "redis://redis:6379/1"}, {"AUDIT_KEY", "short"}, {"SNAPSHOT_KEY", "synthetic-audit-key-0000000000000"},
		{"TARGET_DIRECTORY", "/data/staging/child"}, {"SPOOL_DIRECTORY", "relative"}, {"LOG_MAX_FILES", "0"}, {"STAGING_MAX_BYTES", "0"}, {"TARGET_MAX_BYTES", "-1"}, {"SPOOL_MAX_BYTES", "invalid"},
		{"MANAGEMENT_CREDENTIALS", `[{"operator":"op","token":"synthetic-query-secret","actions":["status"]}]`}, {"MANAGEMENT_CREDENTIALS", `[] {}`}, {"DEPENDENCY_TIMEOUT", "0s"}, {"ENVIRONMENT", "production"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			v := testkit.RuntimeValues("/data")
			v[tc.key] = tc.value
			c, e := config.LoadRuntime(config.Worker, func(k string) (string, bool) { s, ok := v[k]; return s, ok })
			if e == nil {
				t.Fatal("invalid configuration accepted", c)
			}
			if strings.Contains(e.Error(), "synthetic") {
				t.Fatal("secret in error")
			}
		})
	}
}
func TestT15_09RoleSpecificConfigurationAndSafeFormatting(t *testing.T) {
	v := testkit.RuntimeValues("/data")
	for _, role := range []config.Role{config.Query, config.Worker} {
		c, e := config.LoadRuntime(role, func(k string) (string, bool) { s, ok := v[k]; return s, ok })
		if e != nil {
			t.Fatal(e)
		}
		if c.Service.Role != role || c.Service.Timezone != "Asia/Shanghai" || c.StagingBytes != 67108864 || c.MailAddress != "mock-mail:1025" {
			t.Fatal("configuration changed")
		}
		if strings.Contains(fmt.Sprintf("%+v %#v", c, c), "synthetic") {
			t.Fatal("configuration leaked")
		}
	}
	for _, k := range []string{"SOURCE_ORIGIN", "SOURCE_TOKEN", "MAIL_ADDRESS", "MAIL_TLS", "MAIL_TO", "MAIL_FROM", "MANAGEMENT_CREDENTIALS", "STAGING_DIRECTORY", "TARGET_DIRECTORY", "SPOOL_DIRECTORY", "STAGING_MAX_BYTES", "TARGET_MAX_BYTES", "SPOOL_MAX_BYTES"} {
		delete(v, k)
	}
	if _, e := config.LoadRuntime(config.Query, func(k string) (string, bool) { s, ok := v[k]; return s, ok }); e != nil {
		t.Fatal("query unnecessarily requires worker dependencies", e)
	}
}
