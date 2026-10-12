package migrate_test

import (
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
)

func TestT24_05ClassMembershipIndexesUpgradeAndReplay(t *testing.T) {
	plan, err := migrate.Catalog(migrate.Business)
	if err != nil || len(plan) != 4 || plan[3].Version != "business_004" {
		t.Fatal(plan, err)
	}
	for _, want := range []string{
		`CREATE INDEX dwd_bzkspksjzlb_class_cursor_c ON source.dwd_bzkspksjzlb ("JXBH" COLLATE "C", "PKH" COLLATE "C");`,
		`CREATE INDEX dwd_bzkskbsjzlb_class_cursor_c ON source.dwd_bzkskbsjzlb ("JXBH" COLLATE "C", "ID" COLLATE "C");`,
	} {
		if !strings.Contains(plan[3].SQL, want) {
			t.Fatal(want)
		}
	}
	for _, bad := range []string{"DROP ", "CONCURRENTLY", "DELETE ", "ALTER TABLE", "GRANT "} {
		if strings.Contains(plan[3].SQL, bad) {
			t.Fatal("unexpected migration mutation", bad)
		}
	}
	target := migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: storage.Main}}
	for _, fail := range []bool{false, true} {
		db := &database{}
		if n, err := migrate.Apply(t.Context(), db, registry(t), target, plan[:3]); err != nil || n != 3 {
			t.Fatal(n, err)
		}
		if fail {
			db.fail = "class_cursor_c"
		}
		n, err := migrate.Apply(t.Context(), db, registry(t), target, plan)
		if fail {
			if err == nil || n != 0 || len(db.ledger) != 3 || !db.transactions[1].rolledBack {
				t.Fatal(n, err, db.ledger)
			}
			continue
		}
		if err != nil || n != 1 || len(db.ledger) != 4 {
			t.Fatal(n, err, db.ledger)
		}
		for _, old := range plan[:3] {
			if db.ledger[old.Version] != old.Checksum() {
				t.Fatal("old ledger overwritten")
			}
			for _, statement := range db.transactions[1].statements {
				if statement == old.SQL {
					t.Fatal("old DDL replayed")
				}
			}
		}
		if n, err := migrate.Apply(t.Context(), db, registry(t), target, plan); err != nil || n != 0 {
			t.Fatal(n, err)
		}
	}
}
