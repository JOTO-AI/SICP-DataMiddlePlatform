package storage_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func dualValues() map[string]string {
	v := connectionValues()
	v["LOG_STORAGE_MODE"] = "dual"
	v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://upstream:upstream-secret@db-upstream:5432/upstream_logs?sslmode=disable"
	return v
}

func TestT28_01LogRegistryUsesIndependentFixedTargets(t *testing.T) {
	v := dualValues()
	r, err := load(v)
	if err != nil {
		t.Fatal(err)
	}
	for source, key := range map[storage.LogSource]string{storage.PlatformLog: "DATABASE_LOG_URL", storage.UpstreamLog: "DATABASE_UPSTREAM_LOG_URL"} {
		c, err := r.LogConnection(source)
		if err != nil || c.DSN() != v[key] {
			t.Fatal("wrong log target", err)
		}
	}
	if r.LogMode() != storage.DualLogs {
		t.Fatal("dual mode not visible")
	}
	if _, err := r.LogConnection("user-supplied-name"); err == nil {
		t.Fatal("unrestricted target")
	}
	for _, value := range []string{
		"postgres://u:upstream-secret@db-log:5432/sicp_log?sslmode=disable",
		"postgres://u:upstream-secret@db-m:5432/other?sslmode=disable",
		"postgres://u:upstream-secret@db-s:5432/other?sslmode=disable",
	} {
		v := dualValues()
		v["DATABASE_UPSTREAM_LOG_URL"] = value
		if _, err := load(v); err == nil || strings.Contains(err.Error(), "upstream-secret") {
			t.Fatal("overlapping target accepted or disclosed")
		}
	}
}

func TestLogRegistryAllowsSeparateDatabasesOnOneLogInstance(t *testing.T) {
	v := dualValues()
	v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://upstream:upstream-secret@DB-LOG:05432/upstream_logs?sslmode=require"
	r, err := load(v)
	if err != nil {
		t.Fatal("shared log instance rejected", err)
	}
	upstream, err := r.LogConnection(storage.UpstreamLog)
	if err != nil || upstream.DSN() == r.Logs().DSN() {
		t.Fatal("logical log targets collapsed", err)
	}
}

func TestT28_02UpstreamCredentialsAreIndependentAndRedacted(t *testing.T) {
	v := dualValues()
	v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://db-upstream:5432/upstream_logs?sslmode=disable"
	v["DATABASE_UPSTREAM_LOG_USER"] = "upstream-role"
	v["DATABASE_UPSTREAM_LOG_PASSWORD"] = "synthetic-@:/?#%$&=+ 密码"
	r, err := load(v)
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.LogConnection(storage.UpstreamLog)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(c.DSN())
	if err != nil {
		t.Fatal("invalid encoded URL")
	}
	password, ok := u.User.Password()
	if !ok || password != v["DATABASE_UPSTREAM_LOG_PASSWORD"] || u.User.Username() != "upstream-role" {
		t.Fatal("separate credentials changed")
	}
	if r.Logs().DSN() != v["DATABASE_LOG_URL"] {
		t.Fatal("platform credentials changed")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if strings.Contains(fmt.Sprintf(format, r), "synthetic") || strings.Contains(fmt.Sprintf(format, c), "synthetic") {
			t.Fatal("secret formatted")
		}
	}
	for _, mutate := range []func(map[string]string){
		func(v map[string]string) { v["DATABASE_UPSTREAM_LOG_USER"] = "" },
		func(v map[string]string) { delete(v, "DATABASE_UPSTREAM_LOG_PASSWORD") },
		func(v map[string]string) { v["DATABASE_UPSTREAM_LOG_PASSWORD"] = "" },
		func(v map[string]string) { v["DATABASE_UPSTREAM_LOG_URL"] = dualValues()["DATABASE_UPSTREAM_LOG_URL"] },
		func(v map[string]string) {
			v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://db-upstream:5432/upstream_logs?sslmode=disable&host=other"
		},
	} {
		copy := make(map[string]string)
		for k, value := range v {
			copy[k] = value
		}
		mutate(copy)
		if _, err := load(copy); err == nil || strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), "upstream-secret") {
			t.Fatal("invalid credentials accepted or disclosed")
		}
	}
	// Business/platform credentials cannot supply missing upstream credentials.
	v = separateConnectionValues()
	v["LOG_STORAGE_MODE"] = "dual"
	v["DATABASE_UPSTREAM_LOG_URL"] = "postgres://db-upstream:5432/upstream_logs?sslmode=disable"
	if _, err := load(v); err == nil {
		t.Fatal("upstream inherited unrelated credentials")
	}
}

func TestT28_03ExplicitLegacyNeverPretendsToBeDual(t *testing.T) {
	for _, mode := range []string{"", "unknown", "DUAL", " dual"} {
		v := dualValues()
		v["LOG_STORAGE_MODE"] = mode
		if _, err := load(v); err == nil {
			t.Fatal("invalid mode accepted")
		}
	}
	v := dualValues()
	delete(v, "LOG_STORAGE_MODE")
	if _, err := load(v); err == nil {
		t.Fatal("implicit compatibility accepted")
	}
	v = dualValues()
	delete(v, "DATABASE_UPSTREAM_LOG_URL")
	if _, err := load(v); err == nil {
		t.Fatal("missing dual database accepted")
	}
	v = connectionValues()
	r, err := load(v)
	if err != nil || r.LogMode() != storage.SingleLegacy {
		t.Fatal("explicit legacy not visible", err)
	}
	if c, err := r.LogConnection(storage.UpstreamLog); err == nil || c.DSN() != "" {
		t.Fatal("upstream fell back to platform")
	}
	for _, key := range []string{"DATABASE_UPSTREAM_LOG_URL", "DATABASE_UPSTREAM_LOG_USER", "DATABASE_UPSTREAM_LOG_PASSWORD"} {
		v := connectionValues()
		v[key] = "synthetic-stale-value"
		if _, err := load(v); err == nil {
			t.Fatal("ignored upstream setting", key)
		}
	}
	if _, err := storage.LoadRegistry(nil); err == nil {
		t.Fatal("nil lookup accepted")
	}
	if _, err := (storage.Registry{}).LogConnection(storage.PlatformLog); err == nil {
		t.Fatal("zero registry accepted")
	}
}
