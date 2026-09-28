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
	"net/http/cookiejar"
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
		return request(args[1], "GET", "", false)
	case "admin":
		if len(args) < 2 || len(args) > 4 {
			return errors.New("admin action required")
		}
		base := os.Getenv("MANAGEMENT_URL")
		u, err := url.Parse(base)
		if err != nil || u == nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("invalid MANAGEMENT_URL")
		}
		if os.Getenv("ADMIN_USERNAME") == "" || os.Getenv("ADMIN_PASSWORD") == "" {
			return errors.New("ADMIN_USERNAME and ADMIN_PASSWORD required")
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
		return request(base+path, method, key, true)
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
func request(address, method, key string, output bool) error {
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
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if output {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return errors.New("login unavailable")
		}
		client.Jar = jar
		payload, _ := json.Marshal(map[string]string{"username": os.Getenv("ADMIN_USERNAME"), "password": os.Getenv("ADMIN_PASSWORD")})
		base := u.Scheme + "://" + u.Host
		login, _ := http.NewRequestWithContext(ctx, "POST", base+"/console-api/login", strings.NewReader(string(payload)))
		login.Header.Set("Content-Type", "application/json")
		login.Header.Set("X-SICP-Request", "console")
		response, err := client.Do(login)
		if err != nil {
			return errors.New("login unavailable")
		}
		var envelope struct{ Data struct{ CSRF string } }
		err = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&envelope)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || envelope.Data.CSRF == "" {
			return errors.New("administrator login failed")
		}
		r.Header.Set("X-SICP-Request", "console")
		r.Header.Set("X-SICP-CSRF", envelope.Data.CSRF)
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			logout, _ := http.NewRequestWithContext(cleanup, "POST", base+"/console-api/logout", nil)
			logout.Header.Set("X-SICP-Request", "console")
			logout.Header.Set("X-SICP-CSRF", envelope.Data.CSRF)
			if response, err := client.Do(logout); err == nil {
				response.Body.Close()
			}
		}()
	}
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
