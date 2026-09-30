package assembly

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func deploymentValues(t *testing.T, role string) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "deployment", role+".env"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	v, err := config.ReadEnv(f)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestT15_14RepositoryDeploymentFixturesReachActualAssembly(t *testing.T) {
	for _, entry := range []struct {
		name string
		role config.Role
		user string
	}{{"query", config.Query, "sicp_query"}, {"worker", config.Worker, "sicp_worker"}} {
		t.Run(entry.name, func(t *testing.T) {
			v := deploymentValues(t, entry.name)
			c, err := config.LoadRuntime(entry.role, func(k string) (string, bool) { s, ok := v[k]; return s, ok })
			if err != nil {
				t.Fatal(err)
			}
			if c.Service.Timezone != "Asia/Shanghai" || c.Environment != "test" || c.Service.ShutdownTimeout != 20*time.Second || c.LogDirectory != "/var/lib/sicp/logs" || c.LogBytes != 67108864 || c.LogFiles != 10000 || c.RedisURL != "redis://redis:6379/0" {
				t.Fatal("deployment contract drift")
			}
			for _, i := range []storage.Instance{storage.Main, storage.Standby} {
				for slot := 0; slot < 2; slot++ {
					connection, e := c.Databases.Business(storage.Position{Instance: i, Slot: slot})
					if e != nil {
						t.Fatal(e)
					}
					host := "db-m"
					if i == storage.Standby {
						host = "db-s"
					}
					database := "slot0"
					if slot == 1 {
						database = "slot1"
					}
					if connection.DSN() != "postgres://"+entry.user+":synthetic@"+host+":5432/"+database+"?sslmode=disable" {
						t.Fatal("incorrect business role/endpoint")
					}
				}
			}
			if c.Databases.Control().DSN() != "postgres://"+entry.user+":synthetic@db-m:5432/control?sslmode=disable" || c.Databases.Logs().DSN() != "postgres://sicp_logger:synthetic@db-log:5432/logs?sslmode=disable" {
				t.Fatal("fixed endpoint drift")
			}
			if entry.role == config.Worker {
				if c.SourceURL != "http://mock-source:8090" || c.MailAddress != "mock-mail:1025" || c.MailTo != "operator@example.invalid" || c.MailTLS != "none" || c.StagingDirectory != "/var/lib/sicp/staging" || c.TargetDirectory != "/var/lib/sicp/targets" || c.SpoolDirectory != "/var/lib/sicp/spool" || c.StagingBytes != 0 || c.TargetBytes != 0 || c.SpoolBytes != 0 || len(c.Quality) != 14 {
					t.Fatal("worker contract drift")
				}
			}
			// Preserve every deployed value except replacing the asserted absolute
			// volume roots with an isolated unit-test directory. No DB/Redis network.
			root := t.TempDir()
			for _, p := range []*string{&c.LogDirectory, &c.StagingDirectory, &c.TargetDirectory, &c.SpoolDirectory} {
				if *p != "" {
					*p = strings.Replace(*p, "/var/lib/sicp", root, 1)
					if err = os.Mkdir(*p, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			var trace []string
			r, _, err := Open(t.Context(), c, trackedFactories(&trace, ""))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err = r.Close(ctx); err != nil {
				t.Fatal(err)
			}
			want := 6
			if entry.role == config.Worker {
				want = 10
			}
			if len(trace) != want {
				t.Fatal(trace)
			}
		})
	}
}
func TestT15_14InvalidDeploymentCreatesNoDependencies(t *testing.T) {
	for _, key := range []string{"DATABASE_CONTROL_URL", "SOURCE_ORIGIN", "MAIL_ADDRESS", "SOURCE_QUALITY", "ADMIN_USERNAME", "ADMIN_PASSWORD", "CONSOLE_QUERY_URL"} {
		t.Run(key, func(t *testing.T) {
			v := deploymentValues(t, "worker")
			delete(v, key)
			c, err := config.LoadRuntime(config.Worker, func(k string) (string, bool) { s, ok := v[k]; return s, ok })
			if err == nil {
				t.Fatal("missing required setting accepted")
			}
			var trace []string
			if r, _, e := Open(t.Context(), c, trackedFactories(&trace, "")); e == nil || r != nil || len(trace) != 0 {
				t.Fatal("invalid deployment reached dependencies", trace)
			}
		})
	}
}
