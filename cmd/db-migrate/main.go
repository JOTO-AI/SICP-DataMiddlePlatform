// db-migrate is the one-shot database deployment job.
package main

import (
	"context"
	"fmt"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "migration job takes no arguments")
		os.Exit(1)
	}
	if err := migration(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
	if r.LogMode() == storage.DualLogs {
		targets = append(targets, migrate.Target{Domain: migrate.UpstreamLogs})
	}
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
