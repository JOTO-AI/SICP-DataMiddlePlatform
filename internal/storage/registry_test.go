package storage_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func connectionValues() map[string]string {
	return map[string]string{
		"LOG_STORAGE_MODE":     "single_legacy",
		"DATABASE_M0_URL":      "postgres://query:synthetic-secret@db-m:5432/sicp_0?sslmode=disable",
		"DATABASE_M1_URL":      "postgres://query:synthetic-secret@db-m:5432/sicp_1?sslmode=disable",
		"DATABASE_S0_URL":      "postgres://query:synthetic-secret@db-s:5432/sicp_0?sslmode=disable",
		"DATABASE_S1_URL":      "postgres://query:synthetic-secret@db-s:5432/sicp_1?sslmode=disable",
		"DATABASE_CONTROL_URL": "postgres://control:synthetic-secret@db-m:5432/sicp_control?sslmode=disable",
		"DATABASE_LOG_URL":     "postgres://logger:synthetic-secret@db-log:5432/sicp_log?sslmode=disable",
	}
}

func load(values map[string]string) (storage.Registry, error) {
	return storage.LoadRegistry(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
}

func TestT02_01ExplicitBusinessPositions(t *testing.T) {
	values := connectionValues()
	r, err := load(values)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		instance storage.Instance
		slot     int
		key      string
	}{
		{storage.Main, 0, "DATABASE_M0_URL"}, {storage.Main, 1, "DATABASE_M1_URL"},
		{storage.Standby, 0, "DATABASE_S0_URL"}, {storage.Standby, 1, "DATABASE_S1_URL"},
	} {
		c, err := r.Business(storage.Position{Instance: tc.instance, Slot: tc.slot})
		if err != nil || c.DSN() != values[tc.key] {
			t.Fatalf("wrong connection for %s", tc.key)
		}
	}
	for _, p := range []storage.Position{{}, {Instance: "control"}, {Instance: "logs"}, {Instance: "m"}, {Instance: storage.Main, Slot: -1}, {Instance: storage.Standby, Slot: 2}} {
		if c, err := r.Business(p); err == nil || c.DSN() != "" {
			t.Fatal("illegal position returned a connection")
		}
	}
	if _, err := (storage.Registry{}).Business(storage.Position{Instance: storage.Main}); err == nil {
		t.Fatal("zero registry accepted")
	}
}

func TestT02_02FixedConnectionsSurviveRouteChanges(t *testing.T) {
	values := connectionValues()
	r, err := load(values)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []storage.Position{{Instance: storage.Main}, {Instance: storage.Standby}, {Instance: storage.Standby, Slot: 1}, {Instance: storage.Main, Slot: 1}} {
		if _, err := r.Business(p); err != nil {
			t.Fatal(err)
		}
		if r.Control().DSN() != values["DATABASE_CONTROL_URL"] || r.Logs().DSN() != values["DATABASE_LOG_URL"] {
			t.Fatal("fixed connections changed")
		}
	}
	values["DATABASE_CONTROL_URL"] = "changed-after-load"
	if r.Control().DSN() == "changed-after-load" {
		t.Fatal("registry retains mutable input")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if strings.Contains(fmt.Sprintf(format, r), "synthetic-secret") || strings.Contains(fmt.Sprintf(format, r.Control()), "synthetic-secret") {
			t.Fatal("formatting disclosed a secret")
		}
	}
}

func TestControlCanRelocateToLogInstanceWithoutChangingBusinessRoutes(t *testing.T) {
	values := connectionValues()
	values["DATABASE_CONTROL_URL"] = "postgres://control:synthetic-secret@db-log:5432/control?sslmode=disable"
	r, err := load(values)
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		for slot := 0; slot < 2; slot++ {
			c, err := r.Business(storage.Position{Instance: instance, Slot: slot})
			if err != nil || strings.Contains(c.DSN(), "db-log") {
				t.Fatal("control relocation changed a business route", err)
			}
		}
	}
	if r.Control().DSN() != values["DATABASE_CONTROL_URL"] {
		t.Fatal("control did not use the relocated endpoint")
	}
	values["DATABASE_CONTROL_URL"] = values["DATABASE_LOG_URL"]
	if _, err := load(values); err == nil {
		t.Fatal("control and log database overlap accepted")
	}
}

func TestT02_01InvalidConnectionConfiguration(t *testing.T) {
	for _, key := range []string{"DATABASE_M0_URL", "DATABASE_M1_URL", "DATABASE_S0_URL", "DATABASE_S1_URL", "DATABASE_CONTROL_URL", "DATABASE_LOG_URL"} {
		t.Run(key, func(t *testing.T) {
			values := connectionValues()
			delete(values, key)
			if _, err := load(values); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatal("missing target not identified")
			}
		})
	}
	for _, value := range []string{
		"private-secret", "https://query:private-secret@db-m:5432/sicp_0?sslmode=disable",
		"postgres://query:private-secret@db-m/sicp_0?sslmode=disable",
		"postgres://query:private-secret@db-m:65536/sicp_0?sslmode=disable",
		"postgres://query:private-secret@db-m:5432/?sslmode=disable",
		"postgres://query:private-secret@db-m:5432/sicp_0",
		"postgres://query:private-secret@db-m:5432/sicp_0?sslmode=disable&host=other",
		"postgres://query:private-secret@db-m:5432/sicp_0?sslmode=disable&sslmode=require",
		"postgres://query:private-secret@db-m:5432/sicp_0?sslmode=disable#fragment",
		"postgres://query:private-secret@db-m:5432/sicp%2F0?sslmode=disable",
	} {
		values := connectionValues()
		values["DATABASE_M0_URL"] = value
		if _, err := load(values); err == nil || !strings.Contains(err.Error(), "DATABASE_M0_URL") || strings.Contains(err.Error(), "private-secret") {
			t.Fatal("invalid target accepted or secret disclosed")
		}
	}
}

func TestT02_02RejectOverlappingOrMisplacedDatabases(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"DATABASE_M1_URL", "postgres://other:secret@DB-M:05432/sicp_0?sslmode=require"},
		{"DATABASE_M1_URL", "postgres://other:secret@db-other:5432/sicp_1?sslmode=require"},
		{"DATABASE_CONTROL_URL", "postgres://other:secret@db-m:5432/sicp_0?sslmode=require"},
		{"DATABASE_CONTROL_URL", "postgres://other:secret@db-s:5432/control?sslmode=require"},
		{"DATABASE_LOG_URL", "postgres://other:secret@db-m:5432/logs?sslmode=require"},
		{"DATABASE_LOG_URL", "postgres://other:secret@db-s:5432/logs?sslmode=require"},
	} {
		values := connectionValues()
		values[tc.key] = tc.value
		if _, err := load(values); err == nil {
			t.Fatalf("misplaced target accepted: %s", tc.key)
		}
	}
	values := connectionValues()
	values["DATABASE_S0_URL"] = "postgres://other:secret@db-m:5432/standby0?sslmode=require"
	values["DATABASE_S1_URL"] = "postgres://other:secret@db-m:5432/standby1?sslmode=require"
	if _, err := load(values); err == nil {
		t.Fatal("M/S share an endpoint")
	}
}

func separateConnectionValues() map[string]string {
	values := connectionValues()
	for key, raw := range values {
		if !strings.HasSuffix(key, "_URL") {
			continue
		}
		u, _ := url.Parse(raw)
		u.User = nil
		values[key] = u.String()
	}
	values["DATABASE_USER"] = "sicp_query"
	values["DATABASE_PASSWORD"] = "business-@:/?#%$&=+ space-密码"
	values["DATABASE_LOG_USER"] = "sicp_logger"
	values["DATABASE_LOG_PASSWORD"] = "logger-@:/?#%$&=+ space-密码"
	return values
}

func TestSeparateCredentialsEncodeAndReusePasswords(t *testing.T) {
	for _, user := range []string{"sicp_query", "sicp_worker", "admin:@/ name"} {
		t.Run(user, func(t *testing.T) {
			values := separateConnectionValues()
			values["DATABASE_USER"] = user
			r, err := load(values)
			if err != nil {
				t.Fatal(err)
			}
			connections := []storage.Connection{r.Control(), r.Logs()}
			for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
				for slot := 0; slot < 2; slot++ {
					c, err := r.Business(storage.Position{Instance: instance, Slot: slot})
					if err != nil {
						t.Fatal(err)
					}
					connections = append(connections, c)
				}
			}
			for i, c := range connections {
				prefix := "DATABASE_"
				if i == 1 {
					prefix = "DATABASE_LOG_"
				}
				u, err := url.Parse(c.DSN())
				if err != nil {
					t.Fatal("generated URL cannot be parsed")
				}
				password, ok := u.User.Password()
				if !ok || password != values[prefix+"PASSWORD"] || u.User.Username() != values[prefix+"USER"] {
					t.Fatal("credentials did not survive URL encoding")
				}
				u.User = nil
				found := false
				for key, endpoint := range values {
					if strings.HasSuffix(key, "_URL") && u.String() == endpoint {
						found = true
					}
				}
				if !found || strings.Contains(fmt.Sprintf("%+v", c), password) {
					t.Fatal("endpoint changed or secret disclosed")
				}
			}
		})
	}
}

func TestSeparateCredentialsRejectIncompleteOrMixedConfiguration(t *testing.T) {
	for _, key := range []string{"DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_LOG_USER", "DATABASE_LOG_PASSWORD"} {
		for _, empty := range []bool{false, true} {
			values := separateConnectionValues()
			if empty {
				values[key] = ""
			} else {
				delete(values, key)
			}
			if _, err := load(values); err == nil || strings.Contains(err.Error(), "business-") || strings.Contains(err.Error(), "logger-") {
				t.Fatal("incomplete credentials accepted or disclosed", key)
			}
		}
	}
	for key, embedded := range connectionValues() {
		if !strings.HasSuffix(key, "_URL") {
			continue
		}
		values := separateConnectionValues()
		values[key] = embedded
		if _, err := load(values); err == nil || !strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("mixed credentials accepted or disclosed", key)
		}
	}
}
