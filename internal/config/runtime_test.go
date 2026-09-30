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
		{"ADMIN_PASSWORD", "synthetic-query-secret"}, {"ADMIN_PASSWORD", "short"}, {"ADMIN_USERNAME", ""}, {"CONSOLE_QUERY_URL", "http://query:8080/path"}, {"CONSOLE_COOKIE_SECURE", "invalid"}, {"DEPENDENCY_TIMEOUT", "0s"}, {"ENVIRONMENT", "production"},
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
	for _, k := range []string{"SOURCE_ORIGIN", "SOURCE_TOKEN", "MAIL_ADDRESS", "MAIL_TLS", "MAIL_TO", "MAIL_FROM", "ADMIN_USERNAME", "ADMIN_PASSWORD", "CONSOLE_QUERY_URL", "STAGING_DIRECTORY", "TARGET_DIRECTORY", "SPOOL_DIRECTORY", "STAGING_MAX_BYTES", "TARGET_MAX_BYTES", "SPOOL_MAX_BYTES"} {
		delete(v, k)
	}
	if _, e := config.LoadRuntime(config.Query, func(k string) (string, bool) { s, ok := v[k]; return s, ok }); e != nil {
		t.Fatal("query unnecessarily requires worker dependencies", e)
	}
}

func TestRuntimeApplicationCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, environment, appID, secret, token string
		valid                                   bool
	}{
		{"production", "production", "synthetic-app", "synthetic-secret", "", true},
		{"staging", "staging", "synthetic-app", "synthetic-secret", "", true},
		{"missing-id", "production", "", "synthetic-secret", "", false},
		{"missing-secret", "production", "synthetic-app", "", "", false},
		{"mixed-auth", "production", "synthetic-app", "synthetic-secret", "synthetic-token", false},
		{"static-production", "production", "", "", "synthetic-token", false},
		{"static-staging", "staging", "", "", "synthetic-token", false},
		{"static-development", "development", "", "", "synthetic-token", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := testkit.RuntimeValues("/data")
			v["ENVIRONMENT"], v["MAIL_TLS"] = tc.environment, "starttls"
			v["SOURCE_APP_ID"], v["SOURCE_APP_SECRET"], v["SOURCE_TOKEN"] = tc.appID, tc.secret, tc.token
			c, err := config.LoadRuntime(config.Worker, func(k string) (string, bool) { s, ok := v[k]; return s, ok })
			if (err == nil) != tc.valid {
				t.Fatal("unexpected validation", err)
			}
			if strings.Contains(fmt.Sprintf("%+v %#v %v", c, c, err), "synthetic") {
				t.Fatal("credentials leaked")
			}
		})
	}
}

func TestRuntimeSourcePageSize(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"unset", 1000}, {"1", 1}, {"5000", 5000}, {"10000", 10000},
		{"", 0}, {"0", 0}, {"-1", 0}, {"10001", 0}, {"1.5", 0}, {"many", 0},
	} {
		t.Run(tc.value, func(t *testing.T) {
			v := testkit.RuntimeValues("/data")
			if tc.value != "unset" {
				v["SOURCE_PAGE_SIZE"] = tc.value
			}
			lookup := func(k string) (string, bool) { s, ok := v[k]; return s, ok }
			c, err := config.LoadRuntime(config.Worker, lookup)
			if tc.want == 0 {
				if err == nil || !strings.Contains(err.Error(), "SOURCE_PAGE_SIZE") {
					t.Fatal("invalid source page size accepted", err)
				}
			} else if err != nil || c.SourcePageSize != tc.want {
				t.Fatal("source page size not loaded", err)
			}
			if _, err := config.LoadRuntime(config.Query, lookup); err != nil {
				t.Fatal("query requires worker-only page size", err)
			}
		})
	}
}

func TestRuntimeOptionalMail(t *testing.T) {
	mail := map[string]string{
		"MAIL_ADDRESS": "smtp.example.invalid:587", "MAIL_TLS": "starttls",
		"MAIL_FROM": "worker@example.invalid", "MAIL_TO": "operator@example.invalid",
		"MAIL_USERNAME": "synthetic-user", "MAIL_PASSWORD": "synthetic-password",
	}
	for _, environment := range []string{"production", "staging", "test", "development"} {
		t.Run(environment, func(t *testing.T) {
			v := testkit.RuntimeValues("/data")
			v["ENVIRONMENT"] = environment
			v["SOURCE_APP_ID"], v["SOURCE_APP_SECRET"], v["SOURCE_TOKEN"] = "synthetic-app", "synthetic-secret", ""
			load := func() (config.Runtime, error) {
				return config.LoadRuntime(config.Worker, func(k string) (string, bool) { s, ok := v[k]; return s, ok })
			}
			for _, unset := range []bool{false, true} {
				for key := range mail {
					v[key] = ""
					if unset {
						delete(v, key)
					}
				}
				c, err := load()
				if err != nil || c.MailEnabled() {
					t.Fatal("empty mail configuration should disable mail", err)
				}
			}
			for key, value := range mail {
				v[key] = value
				if _, err := load(); err == nil {
					t.Fatalf("single mail setting %s accepted", key)
				}
				delete(v, key)
			}
			for key, value := range mail {
				v[key] = value
			}
			c, err := load()
			if err != nil || !c.MailEnabled() {
				t.Fatal("complete mail configuration should enable mail", err)
			}
			for _, key := range []string{"MAIL_ADDRESS", "MAIL_TLS", "MAIL_FROM", "MAIL_TO", "MAIL_USERNAME", "MAIL_PASSWORD"} {
				v[key] = ""
				if _, err := load(); err == nil {
					t.Fatalf("incomplete mail setting %s accepted", key)
				}
				v[key] = mail[key]
			}
			v["MAIL_USERNAME"], v["MAIL_PASSWORD"] = "", ""
			if _, err := load(); err != nil {
				t.Fatal("SMTP without authentication rejected", err)
			}
		})
	}
}
