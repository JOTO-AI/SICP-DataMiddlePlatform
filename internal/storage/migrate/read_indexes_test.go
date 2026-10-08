package migrate_test

import (
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/migrate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestBT03_ReadIndexMigrationMatchesCatalogAndRollsBack(t *testing.T) {
	plan, err := migrate.Catalog(migrate.Business)
	if err != nil || len(plan) != 3 || plan[2].Version != "business_003" {
		t.Fatal(plan, err)
	}
	for _, table := range source.Catalog() {
		for _, index := range source.ReadIndexes(table.Name) {
			var columns []string
			for _, col := range index.Columns {
				columns = append(columns, `"`+col+`" COLLATE "C"`)
			}
			want := "CREATE INDEX " + index.Name + " ON source." + table.Name + " (" + strings.Join(columns, ", ") + ");"
			if !strings.Contains(plan[2].SQL, want) {
				t.Fatal("missing read index", index.Name)
			}
		}
	}
	if strings.Contains(plan[2].SQL, "CONCURRENTLY") || strings.Contains(plan[2].SQL, "DROP") {
		t.Fatal("unsafe transactional migration")
	}
	db := &database{fail: "CREATE INDEX"}
	if _, err = migrate.Apply(t.Context(), db, registry(t), migrate.Target{Domain: migrate.Business, Position: storage.Position{Instance: storage.Main}}, plan); err == nil || len(db.ledger) != 0 || !db.transactions[0].rolledBack {
		t.Fatal("failed index migration persisted")
	}
}
