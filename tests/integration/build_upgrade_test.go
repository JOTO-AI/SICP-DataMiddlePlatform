//go:build integration

package integration

import (
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestI01_B10ExistingDataUpgradeRollbackAndRetry(t *testing.T) {
	e := newEnvironment(t)
	plan, err := migrate.Catalog(migrate.Business)
	if err != nil || len(plan) != 3 {
		t.Fatal("unexpected migration catalog", err)
	}
	repo := source.Repository{Registry: e.registry, Database: adapters.QueryDatabase{Database: e.db}}
	for slotIndex, position := range []storage.Position{{Instance: storage.Main, Slot: 0}, {Instance: storage.Main, Slot: 1}, {Instance: storage.Standby, Slot: 0}, {Instance: storage.Standby, Slot: 1}} {
		target := migrate.Target{Domain: migrate.Business, Position: position}
		if n, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan[:2]); err != nil || n != 2 {
			t.Fatal(n, err)
		}
		conn := e.connection(t, slotIndex)
		if _, err := conn.Exec(e.ctx, `INSERT INTO source.dwd_bzkscjzlb ("ID","XH","XF") SELECT lpad(i::text,8,'0'),'00001','3.0'::json FROM generate_series(1,1001) i`); err != nil {
			t.Fatal(err)
		}
		// Make migration 003 fail after its first index; neither that index nor
		// its ledger row may survive rollback. Historical checksums stay intact.
		if _, err := conn.Exec(e.ctx, `CREATE INDEX dwd_bzkscjzlb_cursor_c ON source.dwd_bzkscjzlb ("XH")`); err != nil {
			t.Fatal(err)
		}
		if _, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan); err == nil {
			t.Fatal("index collision accepted")
		}
		var rolledBack bool
		if err := conn.QueryRow(e.ctx, `SELECT to_regclass('source.dwd_bzksbjsjzlb_cursor_c') IS NULL AND NOT EXISTS (SELECT 1 FROM migration_meta.versions WHERE version='business_003')`).Scan(&rolledBack); err != nil || !rolledBack {
			t.Fatal("partial migration persisted", err)
		}
		if _, err := conn.Exec(e.ctx, `DROP INDEX source.dwd_bzkscjzlb_cursor_c`); err != nil {
			t.Fatal(err)
		}
		if n, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan); err != nil || n != 1 {
			t.Fatal("retry failed", n, err)
		}
		if n, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan); err != nil || n != 0 {
			t.Fatal("not idempotent", n, err)
		}
		for _, previous := range plan[:2] {
			var checksum string
			if err := conn.QueryRow(e.ctx, "SELECT checksum FROM migration_meta.versions WHERE version=$1", previous.Version).Scan(&checksum); err != nil || checksum != previous.Checksum() {
				t.Fatal("historical checksum changed", err)
			}
		}
		var valid bool
		if err := conn.QueryRow(e.ctx, `SELECT count(*)=1001 AND bool_and("XH"='00001' AND "XF"::text='3.0') FROM source.dwd_bzkscjzlb`).Scan(&valid); err != nil || !valid {
			t.Fatal("migration changed source values", err)
		}
		if err := conn.QueryRow(e.ctx, `SELECT has_table_privilege('sicp_worker','source.dwd_bzkscjzlb','MAINTAIN') AND NOT has_table_privilege('sicp_query','source.dwd_bzkscjzlb','INSERT')`).Scan(&valid); err != nil || !valid {
			t.Fatal("privilege regression", err)
		}
		for _, table := range source.Catalog() {
			if err := repo.CheckPrimaryIndex(e.ctx, position, table.Name); err != nil {
				t.Fatal("index/primary metadata invalid", err)
			}
		}
	}
}
