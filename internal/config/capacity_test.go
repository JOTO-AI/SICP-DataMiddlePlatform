package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestCapacityDefaultsAndRoleIsolation(t *testing.T) {
	values := testkit.RuntimeValues("/data")
	lookup := func(k string) (string, bool) { v, ok := values[k]; return v, ok }
	c, err := config.LoadRuntime(config.Query, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if c.QueryDBBusinessMaxConns != 16 || c.QueryDBControlMaxConns != 16 || c.QueryDBLogMaxConns != 16 ||
		c.QueryMaxExecutions != 8 || c.QueryMaxQueue != 64 || c.QueryExecutionTimeout != 30*time.Second {
		t.Fatal("query defaults changed")
	}
	values["QUERY_MAX_EXECUTIONS"] = "32"
	values["QUERY_MAX_QUEUE"] = "0"
	values["QUERY_EXECUTION_TIMEOUT"] = "45s"
	values["QUERY_DB_BUSINESS_MAX_CONNS"] = "40"
	values["QUERY_DB_CONTROL_MAX_CONNS"] = "48"
	values["QUERY_DB_LOG_MAX_CONNS"] = "12"
	c, err = config.LoadRuntime(config.Query, lookup)
	if err != nil || c.QueryMaxExecutions != 32 || c.QueryMaxQueue != 0 || c.QueryExecutionTimeout != 45*time.Second ||
		c.QueryDBBusinessMaxConns != 40 || c.QueryDBControlMaxConns != 48 || c.QueryDBLogMaxConns != 12 {
		t.Fatal("query overrides lost", err)
	}
	values["QUERY_MAX_EXECUTIONS"] = "invalid"
	values["QUERY_EXECUTION_TIMEOUT"] = "invalid"
	values["QUERY_DB_CONTROL_MAX_CONNS"] = "invalid"
	c, err = config.LoadRuntime(config.Worker, lookup)
	if err != nil || c.QueryDBBusinessMaxConns != 0 || c.QueryDBControlMaxConns != 0 || c.QueryDBLogMaxConns != 0 {
		t.Fatal("query settings affected worker", err)
	}
	values["QUERY_MAX_EXECUTIONS"] = "32"
	values["QUERY_EXECUTION_TIMEOUT"] = "45s"
	values["QUERY_DB_CONTROL_MAX_CONNS"] = "48"
	if c, err = config.LoadRuntime(config.Query, lookup); err != nil {
		t.Fatal("valid query settings rejected", err)
	}
	c.QueryMaxQueue = -1
	if c.Validate() == nil {
		t.Fatal("programmatic invalid capacity accepted")
	}
}

func TestCapacityRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		role    config.Role
		key     string
		invalid []string
	}{
		{config.Query, "QUERY_MAX_EXECUTIONS", []string{"", "0", "-1", "10001", "1.5", "secret-value"}},
		{config.Query, "QUERY_MAX_QUEUE", []string{"", "-1", "100001", "1.5", "secret-value"}},
		{config.Query, "QUERY_EXECUTION_TIMEOUT", []string{"", "0s", "-1s", "61m", "secret-value"}},
		{config.Query, "QUERY_DB_BUSINESS_MAX_CONNS", []string{"", "0", "-1", "1001", "1.5", "secret-value"}},
		{config.Query, "QUERY_DB_CONTROL_MAX_CONNS", []string{"", "0", "1001", "secret-value"}},
		{config.Query, "QUERY_DB_LOG_MAX_CONNS", []string{"", "0", "1001", "secret-value"}},
	} {
		for _, value := range tc.invalid {
			values := testkit.RuntimeValues("/data")
			values[tc.key] = value
			_, err := config.LoadRuntime(tc.role, func(k string) (string, bool) { v, ok := values[k]; return v, ok })
			if err == nil || !strings.Contains(err.Error(), tc.key) || strings.Contains(err.Error(), "secret-value") {
				t.Fatal("invalid setting not rejected safely", tc.key, err)
			}
		}
	}
}

func TestCapacityUpperBounds(t *testing.T) {
	values := testkit.RuntimeValues("/data")
	for key, value := range map[string]string{
		"QUERY_MAX_EXECUTIONS": "10000", "QUERY_MAX_QUEUE": "100000",
		"QUERY_EXECUTION_TIMEOUT": "1h", "QUERY_DB_BUSINESS_MAX_CONNS": "1000",
		"QUERY_DB_CONTROL_MAX_CONNS": "1000", "QUERY_DB_LOG_MAX_CONNS": "1000",
	} {
		values[key] = value
	}
	if _, err := config.LoadRuntime(config.Query, func(k string) (string, bool) { v, ok := values[k]; return v, ok }); err != nil {
		t.Fatal(err)
	}
}
