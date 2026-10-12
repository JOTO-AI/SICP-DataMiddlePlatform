//go:build integration

package integration

import (
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestI11_Phase2IndexUpgradeRollbackAndReadPrivileges(t *testing.T) {
	e := newEnvironment(t)
	plan, err := migrate.Catalog(migrate.Business)
	if err != nil || len(plan) != 4 {
		t.Fatal("unexpected migration plan", err)
	}
	repo := source.Repository{Registry: e.registry, Database: adapters.QueryDatabase{Database: e.db}}
	for index, position := range []storage.Position{{Instance: storage.Main, Slot: 0}, {Instance: storage.Main, Slot: 1}, {Instance: storage.Standby, Slot: 0}, {Instance: storage.Standby, Slot: 1}} {
		target := migrate.Target{Domain: migrate.Business, Position: position}
		if n, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan[:3]); err != nil || n != 3 {
			t.Fatal(n, err)
		}
		conn := e.connection(t, index)
		if _, err := conn.Exec(e.ctx, `INSERT INTO source.dwd_bzkskbsjzlb ("ID","JXBH","JHKCH","XH") VALUES ('0001','J001','C001','00001'); CREATE INDEX dwd_bzkskbsjzlb_class_cursor_c ON source.dwd_bzkskbsjzlb ("XH")`); err != nil {
			t.Fatal(err)
		}
		if _, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan); err == nil {
			t.Fatal("collision accepted")
		}
		var valid bool
		if err := conn.QueryRow(e.ctx, `SELECT to_regclass('source.dwd_bzkspksjzlb_class_cursor_c') IS NULL AND NOT EXISTS(SELECT 1 FROM migration_meta.versions WHERE version='business_004')`).Scan(&valid); err != nil || !valid {
			t.Fatal("partial migration persisted", err)
		}
		if _, err := conn.Exec(e.ctx, `DROP INDEX source.dwd_bzkskbsjzlb_class_cursor_c`); err != nil {
			t.Fatal(err)
		}
		if n, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan); err != nil || n != 1 {
			t.Fatal("upgrade retry", n, err)
		}
		if n, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan); err != nil || n != 0 {
			t.Fatal("restart replay", n, err)
		}
		// The old catalog can reopen the upgraded database; additive indexes
		// and the newer ledger remain. This is not an old-image runtime test.
		if n, err := migrate.Apply(e.ctx, e.db, e.registry, target, plan[:3]); err != nil || n != 0 {
			t.Fatal("legacy catalog rollback", n, err)
		}
		for _, previous := range plan[:3] {
			var checksum string
			if err := conn.QueryRow(e.ctx, "SELECT checksum FROM migration_meta.versions WHERE version=$1", previous.Version).Scan(&checksum); err != nil || checksum != previous.Checksum() {
				t.Fatal("old checksum changed", err)
			}
		}
		for _, table := range source.Catalog() {
			if err := repo.CheckPrimaryIndex(e.ctx, position, table.Name); err != nil {
				t.Fatal(err)
			}
		}
		if err := conn.QueryRow(e.ctx, `SELECT has_table_privilege('sicp_query','source.dwd_bzkskbsjzlb','SELECT') AND NOT has_table_privilege('sicp_query','source.dwd_bzkskbsjzlb','INSERT') AND NOT has_table_privilege('sicp_query','source.dwd_bzkskbsjzlb','UPDATE') AND NOT has_table_privilege('sicp_query','source.dwd_bzkskbsjzlb','DELETE') AND EXISTS(SELECT 1 FROM source.dwd_bzkskbsjzlb WHERE "ID"='0001' AND "XH"='00001')`).Scan(&valid); err != nil || !valid {
			t.Fatal("readonly or source-value regression", err)
		}
	}
}
