package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
)

func lookup(values map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := values[k]; return v, ok }
}
func TestT01_03ConfigDefaultsAndOverrides(t *testing.T) {
	c, err := config.Load(config.Query, lookup(map[string]string{"QUERY_API_KEY": "synthetic-test-key"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Address != "127.0.0.1:8080" || c.ShutdownTimeout != 10*time.Second || c.Timezone != "Asia/Shanghai" {
		t.Fatalf("unexpected defaults: address=%s", c.Address)
	}
	c, err = config.Load(config.Worker, lookup(map[string]string{"HTTP_ADDRESS": "[::1]:9000", "SOURCE_TIMEZONE": "UTC", "SHUTDOWN_TIMEOUT": "250ms", "STARTUP_TIMEOUT": "1s", "HTTP_READ_TIMEOUT": "2s", "HTTP_WRITE_TIMEOUT": "3s", "HTTP_IDLE_TIMEOUT": "4s"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Address != "[::1]:9000" || c.Timezone != "UTC" || c.ShutdownTimeout != 250*time.Millisecond || c.StartupTimeout != time.Second || c.ReadTimeout != 2*time.Second || c.WriteTimeout != 3*time.Second || c.IdleTimeout != 4*time.Second {
		t.Fatal("explicit config lost")
	}
	w, err := config.Load(config.Worker, lookup(nil))
	if err != nil || w.Address != "127.0.0.1:8081" {
		t.Fatal("worker defaults", err)
	}
}

func TestT01_04InvalidConfigurationIsLocatedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
		absent           bool
	}{
		{"missing_key", "QUERY_API_KEY", "", true}, {"empty_key", "QUERY_API_KEY", "", false}, {"blank_key", "QUERY_API_KEY", "  ", false},
		{"negative_timeout", "SHUTDOWN_TIMEOUT", "-1s", false}, {"zero_timeout", "STARTUP_TIMEOUT", "0s", false},
		{"malformed_timeout", "HTTP_READ_TIMEOUT", "private-secret", false}, {"excessive_timeout", "HTTP_IDLE_TIMEOUT", "25h", false},
		{"bad_address", "HTTP_ADDRESS", "private-secret", false}, {"bad_port", "HTTP_ADDRESS", "localhost:65536", false},
		{"empty_timezone", "SOURCE_TIMEZONE", "", false}, {"host_timezone", "SOURCE_TIMEZONE", "Local", false}, {"bad_timezone", "SOURCE_TIMEZONE", "private-secret", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"QUERY_API_KEY": "private-secret"}
			if tc.absent {
				delete(values, tc.key)
			} else {
				values[tc.key] = tc.value
			}
			_, err := config.Load(config.Query, lookup(values))
			if err == nil || !strings.Contains(err.Error(), tc.key) || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("error must identify key without value: %v", err)
			}
		})
	}
	if _, err := config.Load("invalid", lookup(nil)); err == nil {
		t.Fatal("invalid role accepted")
	}
}
