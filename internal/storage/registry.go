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
	Instance Instance `json:"instance"`
	Slot     int      `json:"slot"`
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
	slots          [2][2]Connection
	control        Connection
	logs           Connection
	upstreamLogs   Connection
	logMode        LogStorageMode
	logReaders     map[LogSource]Connection
	logMaintenance map[LogSource]Connection
}

type LogStorageMode string
type LogSource string

const (
	SingleLegacy LogStorageMode = "single_legacy"
	DualLogs     LogStorageMode = "dual"
	PlatformLog  LogSource      = "platform"
	UpstreamLog  LogSource      = "upstream"
)

func (r Registry) LogMode() LogStorageMode { return r.logMode }

func (r Registry) LogReader(source LogSource) (Connection, error) {
	c := r.logReaders[source]
	if c.dsn == "" {
		return c, fmt.Errorf("log reader not configured")
	}
	return c, nil
}
func (r Registry) LogMaintenance(source LogSource) (Connection, error) {
	c := r.logMaintenance[source]
	if c.dsn == "" {
		return c, fmt.Errorf("log maintenance not configured")
	}
	return c, nil
}

// WithServiceLogAccess reuses this process's service login on the fixed log
// targets. Migrations grant query read access and worker read/cleanup access.
// It never substitutes the log writer or provisioning administrator account.
func (r Registry) WithServiceLogAccess(maintenance bool) (Registry, error) {
	service, err := url.Parse(r.control.dsn)
	if err != nil || service.User == nil || service.User.Username() == "" {
		return Registry{}, fmt.Errorf("service database credentials required")
	}
	readers, maintainers := map[LogSource]Connection{}, map[LogSource]Connection{}
	for source, c := range r.logReaders {
		readers[source] = c
	}
	for source, c := range r.logMaintenance {
		if maintenance {
			maintainers[source] = c
		}
	}
	for _, source := range []LogSource{PlatformLog, UpstreamLog} {
		target, err := r.LogConnection(source)
		if err != nil {
			continue
		}
		u, err := url.Parse(target.dsn)
		if err != nil {
			return Registry{}, fmt.Errorf("invalid log target")
		}
		u.User = service.User
		c, err := parseConnection(u.String())
		if err != nil {
			return Registry{}, fmt.Errorf("invalid log service connection")
		}
		if _, ok := readers[source]; !ok {
			readers[source] = c
		}
		if maintenance {
			if _, ok := maintainers[source]; !ok {
				maintainers[source] = c
			}
		}
	}
	r.logReaders, r.logMaintenance = readers, maintainers
	return r, nil
}

// LogConnection never falls back to the platform database for upstream reads
// or writes. Legacy routing remains an explicit decision of the old pipeline.
func (r Registry) LogConnection(source LogSource) (Connection, error) {
	var c Connection
	switch source {
	case PlatformLog:
		c = r.logs
	case UpstreamLog:
		if r.logMode != DualLogs {
			return c, fmt.Errorf("upstream log database not enabled")
		}
		c = r.upstreamLogs
	default:
		return c, fmt.Errorf("invalid log source")
	}
	if c.dsn == "" {
		return c, fmt.Errorf("log database not configured")
	}
	return c, nil
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
// would defeat the isolation checks. VerifyLogIsolation checks physical database
// identities at deployment; URL comparison alone cannot detect DNS aliases.
func LoadRegistry(lookup func(string) (string, bool)) (Registry, error) {
	var r Registry
	if lookup == nil {
		return r, fmt.Errorf("database lookup required")
	}
	mode, _ := lookup("LOG_STORAGE_MODE")
	r.logMode = LogStorageMode(mode)
	if r.logMode != SingleLegacy && r.logMode != DualLogs {
		return Registry{}, fmt.Errorf("LOG_STORAGE_MODE: explicit single_legacy or dual required")
	}
	entries := []struct {
		key    string
		target *Connection
	}{
		{"DATABASE_M0_URL", &r.slots[0][0]}, {"DATABASE_M1_URL", &r.slots[0][1]},
		{"DATABASE_S0_URL", &r.slots[1][0]}, {"DATABASE_S1_URL", &r.slots[1][1]},
		{"DATABASE_CONTROL_URL", &r.control}, {"DATABASE_LOG_URL", &r.logs},
	}
	if r.logMode == DualLogs {
		entries = append(entries, struct {
			key    string
			target *Connection
		}{"DATABASE_UPSTREAM_LOG_URL", &r.upstreamLogs})
	} else {
		for _, key := range []string{"DATABASE_UPSTREAM_LOG_URL", "DATABASE_UPSTREAM_LOG_USER", "DATABASE_UPSTREAM_LOG_PASSWORD"} {
			if v, _ := lookup(key); v != "" {
				return Registry{}, fmt.Errorf("LOG_STORAGE_MODE: upstream settings require dual mode")
			}
		}
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		c, err := configuredConnection(lookup, entry.key)
		if err != nil {
			return Registry{}, fmt.Errorf("%s: invalid PostgreSQL endpoint or credentials", entry.key)
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
	// Keep the M endpoint readable during the explicit relocation workflow.
	// New deployments place the distinct control database on db-log.
	if r.control.endpoint != r.slots[0][0].endpoint && r.control.endpoint != r.logs.endpoint {
		return Registry{}, fmt.Errorf("DATABASE_CONTROL_URL: control database must use the fixed M or platform-log endpoint")
	}
	if r.logs.endpoint == r.slots[0][0].endpoint || r.logs.endpoint == r.slots[1][0].endpoint {
		return Registry{}, fmt.Errorf("DATABASE_LOG_URL: logs require an independent endpoint")
	}
	if r.logMode == DualLogs && (r.upstreamLogs.endpoint == r.slots[0][0].endpoint || r.upstreamLogs.endpoint == r.slots[1][0].endpoint) {
		return Registry{}, fmt.Errorf("DATABASE_UPSTREAM_LOG_URL: upstream logs require an endpoint separate from M/S")
	}
	r.logReaders = map[LogSource]Connection{}
	for _, source := range []LogSource{PlatformLog, UpstreamLog} {
		key := "DATABASE_LOG_READ_URL"
		if source == UpstreamLog {
			key = "DATABASE_UPSTREAM_LOG_READ_URL"
		}
		if raw, _ := lookup(key); raw != "" {
			c, err := configuredConnection(lookup, key)
			target, e := r.LogConnection(source)
			if err != nil || e != nil || c.endpoint != target.endpoint || c.database != target.database || c.dsn == target.dsn {
				return Registry{}, fmt.Errorf("%s: independent reader credentials on matching target required", key)
			}
			r.logReaders[source] = c
		}
	}
	r.logMaintenance = map[LogSource]Connection{}
	for _, source := range []LogSource{PlatformLog, UpstreamLog} {
		key := "DATABASE_LOG_MAINTENANCE_URL"
		if source == UpstreamLog {
			key = "DATABASE_UPSTREAM_LOG_MAINTENANCE_URL"
		}
		if raw, _ := lookup(key); raw != "" {
			c, err := parseConnection(raw)
			target, e := r.LogConnection(source)
			if err != nil || e != nil || c.endpoint != target.endpoint || c.database != target.database || c.dsn == target.dsn {
				return Registry{}, fmt.Errorf("%s: matching target and independent maintenance credentials required", key)
			}
			r.logMaintenance[source] = c
		}
	}
	return r, nil
}

// Compose supplies password-free endpoints and role-specific credentials. Keep
// full URLs working for direct process deployments, but reject mixed forms so a
// stale embedded password cannot silently override the separately supplied one.
func configuredConnection(lookup func(string) (string, bool), key string) (Connection, error) {
	raw, _ := lookup(key)
	prefix := "DATABASE_"
	if key == "DATABASE_LOG_URL" {
		prefix = "DATABASE_LOG_"
	} else if key == "DATABASE_UPSTREAM_LOG_URL" {
		prefix = "DATABASE_UPSTREAM_LOG_"
	} else if key == "DATABASE_LOG_READ_URL" {
		prefix = "DATABASE_LOG_READ_"
	} else if key == "DATABASE_UPSTREAM_LOG_READ_URL" {
		prefix = "DATABASE_UPSTREAM_LOG_READ_"
	}
	user, hasUser := lookup(prefix + "USER")
	password, hasPassword := lookup(prefix + "PASSWORD")
	if hasUser || hasPassword {
		u, err := url.Parse(raw)
		if !hasUser || !hasPassword || user == "" || password == "" || err != nil || u.User != nil || strings.TrimSpace(raw) != raw {
			return Connection{}, fmt.Errorf("invalid separate database credentials or endpoint")
		}
		u.User = url.UserPassword(user, password)
		raw = u.String()
	}
	return parseConnection(raw)
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
