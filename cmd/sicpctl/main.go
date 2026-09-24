// sicpctl provides explicit deployment operations; it never creates tasks while
// validating configuration or migrating an empty installation.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sicpctl validate ROLE | migrate | health URL | admin ACTION [KEY] [TASK]")
	}
	switch args[0] {
	case "validate":
		if len(args) != 2 {
			return errors.New("explicit service role required")
		}
		_, err := config.LoadRuntime(config.Role(args[1]), os.LookupEnv)
		if err == nil {
			fmt.Println("configuration valid")
		}
		return err
	case "migrate":
		if len(args) != 1 {
			return errors.New("migrate takes no arguments")
		}
		return migration()
	case "health":
		if len(args) != 2 {
			return errors.New("explicit health URL required")
		}
		return request(args[1], "GET", "", "", false)
	case "admin":
		if len(args) < 2 || len(args) > 4 {
			return errors.New("admin action required")
		}
		base := os.Getenv("MANAGEMENT_URL")
		u, err := url.Parse(base)
		if err != nil || u == nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("invalid MANAGEMENT_URL")
		}
		token := os.Getenv("MANAGEMENT_TOKEN")
		if strings.TrimSpace(token) == "" {
			return errors.New("MANAGEMENT_TOKEN required")
		}
		path := "/admin/v1/tasks/"
		method := "POST"
		key := ""
		switch args[1] {
		case "initialize", "sync", "force-full":
			if len(args) != 3 {
				return errors.New("stable idempotency key required")
			}
			key = args[2]
			path += args[1]
		case "rerun":
			if len(args) != 4 || !identifier(args[3]) {
				return errors.New("idempotency key and task ID required")
			}
			key = args[2]
			path += url.PathEscape(args[3]) + "/rerun"
		case "status":
			if len(args) != 3 || !identifier(args[2]) {
				return errors.New("task ID required")
			}
			method = "GET"
			path += url.PathEscape(args[2])
		default:
			return errors.New("unknown management action")
		}
		if method == "POST" && !identifier(key) {
			return errors.New("invalid idempotency key")
		}
		return request(base+path, method, token, key, true)
	default:
		return errors.New("unknown operation")
	}
}
func identifier(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func request(address, method, token, key string, output bool) error {
	u, e := url.Parse(address)
	if e != nil || u == nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("invalid endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := http.NewRequestWithContext(ctx, method, address, nil)
	if e != nil {
		return errors.New("invalid request")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(r)
	if e != nil {
		return errors.New("management or health endpoint unavailable")
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if e != nil || len(body) > 1<<20 {
		return errors.New("invalid response")
	}
	if output {
		if !json.Valid(body) {
			return errors.New("invalid management response")
		}
		if _, e = os.Stdout.Write(body); e != nil {
			return errors.New("output failed")
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}
func migration() error {
	r, e := storage.LoadRegistry(os.LookupEnv)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, e := adapters.OpenPostgres(ctx, r, 10*time.Second, "migration")
	if db != nil {
		defer func() {
			c, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = db.Close(c)
		}()
	}
	if e != nil {
		return e
	}
	var targets []migrate.Target
	for _, i := range []storage.Instance{storage.Main, storage.Standby} {
		for slot := 0; slot < 2; slot++ {
			targets = append(targets, migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: i, Slot: slot}})
		}
	}
	targets = append(targets, migrate.Target{Domain: migrate.Control}, migrate.Target{Domain: migrate.Logs})
	for _, target := range targets {
		plan, e := migrate.Catalog(target.Domain)
		if e != nil {
			return e
		}
		n, e := migrate.Apply(ctx, db, r, target, plan)
		if e != nil {
			return e
		}
		fmt.Printf("%s %s/%d: %d migrations applied\n", target.Domain, target.Position.Instance, target.Position.Slot, n)
	}
	return nil
}
