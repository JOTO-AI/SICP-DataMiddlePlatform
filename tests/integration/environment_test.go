//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"github.com/jackc/pgx/v5"
)

// Each case owns six databases; none can inherit another case's state. Outer
// Actions cleanup destroys the entire Compose project even after a test crash.
type environment struct {
	ctx      context.Context
	registry storage.Registry
	db       *adapters.Postgres
	dsns     []string
}

func newEnvironment(t *testing.T) *environment {
	t.Helper()
	if os.Getenv("SICP_INTEGRATION") != "1" || os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Fatal("integration requires the isolated GitHub Actions runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	e := &environment{ctx: ctx}
	prefix := fmt.Sprintf("it_%x", rand.Text()[:12])
	keys := []string{"DATABASE_M0_URL", "DATABASE_M1_URL", "DATABASE_S0_URL", "DATABASE_S1_URL", "DATABASE_CONTROL_URL", "DATABASE_LOG_URL"}
	hosts := map[string]string{}
	for _, cluster := range []string{"M", "S", "LOG"} {
		host := os.Getenv("SICP_TEST_" + cluster + "_HOST")
		if ip := net.ParseIP(host); ip == nil || !ip.IsPrivate() {
			t.Fatal("integration requires an explicit private Compose container address")
		}
		hosts[cluster] = host
	}
	clusters := []string{"M", "M", "S", "S", "M", "LOG"}
	values := map[string]string{}
	for i, key := range keys {
		name := fmt.Sprintf("%s_%d", prefix, i)
		endpoint := net.JoinHostPort(hosts[clusters[i]], "5432")
		admin := fmt.Sprintf("postgres://postgres:synthetic@%s/postgres?sslmode=disable", endpoint)
		// Register recovery before CREATE, including an ambiguous creation response.
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			conn, err := pgx.Connect(cleanup, admin)
			if err != nil {
				t.Errorf("cleanup connect: %v", err)
				return
			}
			defer conn.Close(cleanup)
			if _, err := conn.Exec(cleanup, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
				t.Errorf("cleanup database: %v", err)
			}
		})
		conn, err := pgx.Connect(ctx, admin)
		if err != nil {
			t.Fatal(err)
		}
		_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
		conn.Close(ctx)
		if err != nil {
			t.Fatal(err)
		}
		dsn := fmt.Sprintf("postgres://postgres:synthetic@%s/%s?sslmode=disable", endpoint, name)
		values[key] = dsn
		e.dsns = append(e.dsns, dsn)
	}
	var err error
	e.registry, err = storage.LoadRegistry(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	e.db, err = adapters.OpenPostgres(ctx, e.registry, 5*time.Second)
	if e.db != nil {
		t.Cleanup(func() {
			if err := e.db.Close(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *environment) migrate(t *testing.T) {
	t.Helper()
	for _, target := range targets() {
		plan, err := migrate.Catalog(target.Domain)
		if err != nil {
			t.Fatal(err)
		}
		n, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan)
		if err != nil || n != len(plan) {
			t.Fatalf("migrate %v: applied=%d error=%v", target, n, err)
		}
	}
}
func targets() []migrate.Target {
	result := []migrate.Target{}
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		for slot := 0; slot < 2; slot++ {
			result = append(result, migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: instance, Slot: slot}})
		}
	}
	return append(result, migrate.Target{Domain: migrate.Control}, migrate.Target{Domain: migrate.Logs})
}
func (e *environment) connection(t *testing.T, index int) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(e.ctx, e.dsns[index])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := conn.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return conn
}
