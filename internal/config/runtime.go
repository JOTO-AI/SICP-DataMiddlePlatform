package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// Runtime is the complete deployment contract. No network or filesystem access
// occurs while loading it. String deliberately excludes endpoints and secrets.
type Runtime struct {
	Service                                                         Config
	Databases                                                       storage.Registry
	RedisURL, SourceURL, SourceToken                                string
	MailAddress, MailTLS, MailUser, MailPassword, MailFrom, MailTo  string
	LogDirectory, StagingDirectory, TargetDirectory, SpoolDirectory string
	LogBytes, LogFiles, StagingBytes, TargetBytes, SpoolBytes       int64
	Credentials                                                     []admin.Credential
	SnapshotKey, AuditKey, CodeVersion, Environment                 string
	DependencyTimeout                                               time.Duration
}

func (Runtime) String() string     { return "[runtime configuration redacted]" }
func (c Runtime) GoString() string { return c.String() }

func LoadRuntime(role Role, lookup func(string) (string, bool)) (Runtime, error) {
	c := Runtime{DependencyTimeout: 10 * time.Second}
	var err error
	if c.Service, err = Load(role, lookup); err != nil {
		return Runtime{}, err
	}
	if c.Databases, err = storage.LoadRegistry(lookup); err != nil {
		return Runtime{}, err
	}
	for _, e := range []struct {
		k string
		p *string
	}{
		{"REDIS_URL", &c.RedisURL}, {"SOURCE_ORIGIN", &c.SourceURL}, {"SOURCE_TOKEN", &c.SourceToken},
		{"MAIL_ADDRESS", &c.MailAddress}, {"MAIL_TLS", &c.MailTLS}, {"MAIL_USERNAME", &c.MailUser}, {"MAIL_PASSWORD", &c.MailPassword}, {"MAIL_FROM", &c.MailFrom}, {"MAIL_TO", &c.MailTo},
		{"LOG_DIRECTORY", &c.LogDirectory}, {"STAGING_DIRECTORY", &c.StagingDirectory}, {"TARGET_DIRECTORY", &c.TargetDirectory}, {"SPOOL_DIRECTORY", &c.SpoolDirectory},
		{"SNAPSHOT_KEY", &c.SnapshotKey}, {"AUDIT_KEY", &c.AuditKey}, {"CODE_VERSION", &c.CodeVersion}, {"ENVIRONMENT", &c.Environment},
	} {
		*e.p, _ = lookup(e.k)
	}
	for _, e := range []struct {
		k string
		p *int64
	}{
		{"LOG_MAX_BYTES", &c.LogBytes}, {"LOG_MAX_FILES", &c.LogFiles}, {"STAGING_MAX_BYTES", &c.StagingBytes}, {"TARGET_MAX_BYTES", &c.TargetBytes}, {"SPOOL_MAX_BYTES", &c.SpoolBytes},
	} {
		v, ok := lookup(e.k)
		if !ok && role == Query && strings.HasPrefix(e.k, "LOG_") == false {
			continue
		}
		*e.p, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Runtime{}, fmt.Errorf("%s: explicit integer required", e.k)
		}
	}
	if v, ok := lookup("DEPENDENCY_TIMEOUT"); ok {
		c.DependencyTimeout, err = time.ParseDuration(v)
		if err != nil {
			return Runtime{}, fmt.Errorf("DEPENDENCY_TIMEOUT: invalid duration")
		}
	}
	if role == Worker {
		raw, _ := lookup("MANAGEMENT_CREDENTIALS")
		d := json.NewDecoder(strings.NewReader(raw))
		d.DisallowUnknownFields()
		if len(raw) > 1<<20 || d.Decode(&c.Credentials) != nil || d.Decode(new(any)) != io.EOF {
			return Runtime{}, fmt.Errorf("MANAGEMENT_CREDENTIALS: invalid configuration")
		}
	}
	if err = c.Validate(); err != nil {
		return Runtime{}, err
	}
	return c, nil
}

func endpoint(raw string, schemes ...string) (*url.URL, bool) {
	u, e := url.Parse(raw)
	if e != nil || u == nil || u.Hostname() == "" || u.Opaque != "" || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || strings.TrimSpace(raw) != raw {
		return nil, false
	}
	ok := false
	for _, s := range schemes {
		ok = ok || u.Scheme == s
	}
	return u, ok
}
func (c Runtime) Validate() error {
	bad := func(k string) error { return fmt.Errorf("%s: invalid or missing runtime configuration", k) }
	if err := c.Service.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Service.APIKey) == "" {
		return bad("QUERY_API_KEY")
	}
	// Registry construction is the only way to obtain nonempty connections.
	for _, i := range []storage.Instance{storage.Main, storage.Standby} {
		for s := 0; s < 2; s++ {
			if _, e := c.Databases.Business(storage.Position{Instance: i, Slot: s}); e != nil {
				return bad("DATABASE")
			}
		}
	}
	if c.Databases.Control().DSN() == "" || c.Databases.Logs().DSN() == "" {
		return bad("DATABASE")
	}
	u, ok := endpoint(c.RedisURL, "redis", "rediss")
	if !ok || !validAddress(u.Host) || u.Path != "/0" {
		return bad("REDIS_URL")
	}
	if c.DependencyTimeout < time.Millisecond || c.DependencyTimeout > time.Minute {
		return bad("DEPENDENCY_TIMEOUT")
	}
	if len(c.SnapshotKey) < 32 || len(c.AuditKey) < 32 || c.SnapshotKey == c.AuditKey || c.SnapshotKey == c.Service.APIKey || c.AuditKey == c.Service.APIKey {
		return bad("SNAPSHOT_KEY/AUDIT_KEY")
	}
	if strings.TrimSpace(c.CodeVersion) == "" || len(c.CodeVersion) > 128 {
		return bad("CODE_VERSION")
	}
	if c.Environment != "test" && c.Environment != "development" && c.Environment != "staging" && c.Environment != "production" {
		return bad("ENVIRONMENT")
	}
	if c.LogBytes < 1024 || c.LogBytes > 1<<40 || c.LogFiles < 2 || c.LogFiles > 1000000 {
		return bad("LOG_CAPACITY")
	}
	paths := []string{c.LogDirectory}
	if c.Service.Role == Worker {
		if _, e := admin.NewAuth(c.Service.APIKey, c.Credentials); e != nil {
			return bad("MANAGEMENT_CREDENTIALS")
		}
		u, ok = endpoint(c.SourceURL, "http", "https")
		if !ok || u.User != nil || (u.Path != "" && u.Path != "/") || strings.TrimSpace(c.SourceToken) == "" {
			return bad("SOURCE_ORIGIN/SOURCE_TOKEN")
		}
		if !validAddress(c.MailAddress) || (c.MailTLS != "starttls" && c.MailTLS != "tls" && c.MailTLS != "none") {
			return bad("MAIL_ADDRESS/MAIL_TLS")
		}
		if (c.MailUser == "") != (c.MailPassword == "") || (c.MailTLS == "none" && (c.Environment == "production" || c.MailUser != "")) {
			return bad("MAIL_CREDENTIALS")
		}
		for _, s := range []string{c.MailFrom, c.MailTo} {
			a, e := mail.ParseAddress(s)
			if e != nil || a.Address != s || strings.ContainsAny(s, "\r\n") || len(s) > 254 {
				return bad("MAIL_FROM/MAIL_TO")
			}
		}
		for _, v := range []int64{c.StagingBytes, c.TargetBytes, c.SpoolBytes} {
			if v < 1<<20 || v > 1<<40 {
				return bad("FILE_CAPACITY")
			}
		}
		paths = append(paths, c.StagingDirectory, c.TargetDirectory, c.SpoolDirectory)
	}
	for i, p := range paths {
		if !path.IsAbs(p) || path.Clean(p) != p || p == "/" || strings.ContainsAny(p, "\\\x00") {
			return bad("PERSISTENT_DIRECTORY")
		}
		for _, other := range paths[:i] {
			if p == other || strings.HasPrefix(p, other+"/") || strings.HasPrefix(other, p+"/") {
				return bad("PERSISTENT_DIRECTORY_OVERLAP")
			}
		}
	}
	return nil
}
func validAddress(v string) bool {
	h, p, e := net.SplitHostPort(v)
	n, x := strconv.Atoi(p)
	return e == nil && h != "" && x == nil && n > 0 && n <= 65535
}
