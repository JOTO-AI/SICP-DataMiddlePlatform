// Package storage defines explicit business and fixed database boundaries.
package storage

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Instance string

const (
	Main    Instance = "M"
	Standby Instance = "S"
)

type Position struct {
	Instance Instance
	Slot     int
}

func (p Position) Validate() error {
	if (p.Instance != Main && p.Instance != Standby) || p.Slot < 0 || p.Slot > 1 {
		return fmt.Errorf("invalid business position")
	}
	return nil
}

// Connection is immutable. DSN is an explicit secret access for adapter creation;
// ordinary formatting must never disclose credentials.
type Connection struct{ dsn, endpoint, database string }

func (c Connection) DSN() string      { return c.dsn }
func (c Connection) String() string   { return "[database connection redacted]" }
func (c Connection) GoString() string { return c.String() }

// Registry has no mutable active route. Callers supply their pinned position.
// Fixed connections cannot be selected through the business lookup.
type Registry struct {
	slots   [2][2]Connection
	control Connection
	logs    Connection
}

func (r Registry) Business(p Position) (Connection, error) {
	if err := p.Validate(); err != nil {
		return Connection{}, err
	}
	i := 0
	if p.Instance == Standby {
		i = 1
	}
	c := r.slots[i][p.Slot]
	if c.dsn == "" {
		return Connection{}, fmt.Errorf("database registry not configured")
	}
	return c, nil
}

func (r Registry) Control() Connection { return r.control }
func (r Registry) Logs() Connection    { return r.logs }
func (r Registry) String() string      { return "[database registry redacted]" }
func (r Registry) GoString() string    { return r.String() }

// LoadRegistry validates all targets before any adapter is created. PostgreSQL
// URLs are deliberately restricted: routing overrides and implicit defaults
// would defeat the isolation checks. DNS aliases remain a deployment concern.
func LoadRegistry(lookup func(string) (string, bool)) (Registry, error) {
	var r Registry
	entries := []struct {
		key    string
		target *Connection
	}{
		{"DATABASE_M0_URL", &r.slots[0][0]}, {"DATABASE_M1_URL", &r.slots[0][1]},
		{"DATABASE_S0_URL", &r.slots[1][0]}, {"DATABASE_S1_URL", &r.slots[1][1]},
		{"DATABASE_CONTROL_URL", &r.control}, {"DATABASE_LOG_URL", &r.logs},
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		value, _ := lookup(entry.key)
		c, err := parseConnection(value)
		if err != nil {
			return Registry{}, fmt.Errorf("%s: invalid explicit PostgreSQL URL", entry.key)
		}
		identity := c.endpoint + "/" + c.database
		if seen[identity] {
			return Registry{}, fmt.Errorf("%s: database target overlaps another connection", entry.key)
		}
		seen[identity] = true
		*entry.target = c
	}
	if r.slots[0][0].endpoint != r.slots[0][1].endpoint || r.slots[1][0].endpoint != r.slots[1][1].endpoint {
		return Registry{}, fmt.Errorf("DATABASE_M1_URL/DATABASE_S1_URL: slots must share their instance endpoint")
	}
	if r.slots[0][0].endpoint == r.slots[1][0].endpoint {
		return Registry{}, fmt.Errorf("DATABASE_S0_URL: M and S require independent endpoints")
	}
	if r.control.endpoint != r.slots[0][0].endpoint {
		return Registry{}, fmt.Errorf("DATABASE_CONTROL_URL: control database must use the fixed M endpoint")
	}
	if r.logs.endpoint == r.slots[0][0].endpoint || r.logs.endpoint == r.slots[1][0].endpoint {
		return Registry{}, fmt.Errorf("DATABASE_LOG_URL: logs require an independent endpoint")
	}
	return r, nil
}

func parseConnection(raw string) (Connection, error) {
	bad := fmt.Errorf("invalid connection")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Opaque != "" || u.Fragment != "" || u.User == nil || u.User.Username() == "" {
		return Connection{}, bad
	}
	if strings.TrimSpace(raw) != raw || strings.ContainsAny(u.Host, ", \t\r\n") {
		return Connection{}, bad
	}
	host := strings.ToLower(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		return Connection{}, bad
	}
	database := strings.TrimPrefix(u.Path, "/")
	if database == "" || strings.ContainsAny(database, "/\\\\\x00") {
		return Connection{}, bad
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["sslmode"]) != 1 {
		return Connection{}, bad
	}
	switch q.Get("sslmode") {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return Connection{}, bad
	}
	return Connection{dsn: raw, endpoint: net.JoinHostPort(host, strconv.Itoa(port)), database: database}, nil
}
