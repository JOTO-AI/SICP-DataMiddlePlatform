package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type Role string

const (
	Query  Role = "query-service"
	Worker Role = "sync-worker"
)

type Config struct {
	Role            Role
	Address         string
	APIKey          string
	Timezone        string
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
}

// Load never includes supplied values in errors, including malformed secrets.
// Infrastructure credentials are introduced with their owning P02/P03/P05 tasks.
func Load(role Role, lookup func(string) (string, bool)) (Config, error) {
	c := Config{Role: role, Address: "127.0.0.1:8080", Timezone: "Asia/Shanghai", StartupTimeout: 15 * time.Second, ShutdownTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	if role == Worker {
		c.Address = "127.0.0.1:8081"
	}
	if role != Query && role != Worker {
		return Config{}, fmt.Errorf("SERVICE_ROLE: invalid role")
	}
	if v, ok := lookup("HTTP_ADDRESS"); ok {
		c.Address = v
	}
	if v, ok := lookup("SOURCE_TIMEZONE"); ok {
		c.Timezone = v
	}
	c.APIKey, _ = lookup("QUERY_API_KEY")
	for _, entry := range []struct {
		name string
		dst  *time.Duration
	}{
		{"STARTUP_TIMEOUT", &c.StartupTimeout}, {"SHUTDOWN_TIMEOUT", &c.ShutdownTimeout},
		{"HTTP_READ_TIMEOUT", &c.ReadTimeout}, {"HTTP_WRITE_TIMEOUT", &c.WriteTimeout}, {"HTTP_IDLE_TIMEOUT", &c.IdleTimeout},
	} {
		if v, ok := lookup(entry.name); ok {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 || d > 24*time.Hour {
				return Config{}, fmt.Errorf("%s: expected duration in (0,24h]", entry.name)
			}
			*entry.dst = d
		}
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.Role != Query && c.Role != Worker {
		return fmt.Errorf("SERVICE_ROLE: invalid role")
	}
	if c.Role == Query && strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("QUERY_API_KEY: required")
	}
	_, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("HTTP_ADDRESS: expected host:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("HTTP_ADDRESS: invalid port")
	}
	if c.Timezone == "" || c.Timezone == "Local" {
		return fmt.Errorf("SOURCE_TIMEZONE: explicit IANA timezone required")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("SOURCE_TIMEZONE: invalid IANA timezone")
	}
	for _, entry := range []struct {
		name  string
		value time.Duration
	}{
		{"STARTUP_TIMEOUT", c.StartupTimeout}, {"SHUTDOWN_TIMEOUT", c.ShutdownTimeout},
		{"HTTP_READ_TIMEOUT", c.ReadTimeout}, {"HTTP_WRITE_TIMEOUT", c.WriteTimeout}, {"HTTP_IDLE_TIMEOUT", c.IdleTimeout},
	} {
		if entry.value <= 0 || entry.value > 24*time.Hour {
			return fmt.Errorf("%s: expected duration in (0,24h]", entry.name)
		}
	}
	return nil
}
