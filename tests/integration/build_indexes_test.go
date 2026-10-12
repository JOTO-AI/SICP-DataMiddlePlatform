//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type baselineMatcher struct{ source.Repository }

func (m baselineMatcher) ReadMatching(ctx context.Context, h read.Handle, table, after string, n int, filters []source.Filter) ([]source.SourceRow, error) {
	return m.Repository.ReadMatching(ctx, h.Position, table, after, n, filters)
}

func TestI01_B03IndexesAndB02LargeScan(t *testing.T) {
	e := newEnvironmentWithLocale(t, 5*time.Minute, "en_US.utf8")
	e.migrate(t)
	repo := source.Repository{Registry: e.registry, Database: adapters.QueryDatabase{Database: e.db}}
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		for slot := 0; slot < 2; slot++ {
			for _, table := range source.Catalog() {
				if err := repo.CheckPrimaryIndex(e.ctx, storage.Position{Instance: instance, Slot: slot}, table.Name); err != nil {
					t.Fatal("invalid migrated read index", err)
				}
			}
		}
	}
	conn := e.connection(t, 0)
	if _, err := conn.Exec(e.ctx, `INSERT INTO source.dwd_bzkscjzlb ("ID","XH") SELECT lpad(i::text,9,'0'),'S'||lpad((i%1000)::text,4,'0') FROM generate_series(1,100001) i; ANALYZE source.dwd_bzkscjzlb`); err != nil {
		t.Fatal(err)
	}
	h := read.Handle{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 1, Combination: "synthetic"}
	for _, background := range []bool{false, true} {
		limits := read.DefaultLimits()
		if background {
			limits, _ = read.TableLimits(100001, 3*time.Minute)
		}
		budget, err := read.NewBudget(limits, ports.SystemClock{})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		err = read.Scan(e.ctx, baselineMatcher{repo}, h, "dwd_bzkscjzlb", nil, budget, func(source.SourceRow) error { count++; return nil })
		if background && (err != nil || count != 100001) {
			t.Fatal(count, err)
		}
		if !background && !errors.Is(err, read.ErrBudget) {
			t.Fatal("frontend lost budget", err)
		}
	}
	keys := []string{"001", "0001", "A", "a", "中", "é"}
	for _, key := range keys {
		if _, err := conn.Exec(e.ctx, `INSERT INTO source.dwd_xqjbsjzlb ("XQH") VALUES ($1)`, key); err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(keys)
	var actual []string
	pageLimits := read.BackgroundLimits(time.Minute)
	pageLimits.BatchSize = 2
	budget, _ := read.NewBudget(pageLimits, ports.SystemClock{})
	if err := read.Scan(e.ctx, baselineMatcher{repo}, h, "dwd_xqjbsjzlb", nil, budget, func(row source.SourceRow) error {
		var key string
		err := json.Unmarshal(row["XQH"], &key)
		actual = append(actual, key)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(keys, "|") != strings.Join(actual, "|") {
		t.Fatal("C order differs")
	}
	var plans []json.RawMessage
	for _, after := range []string{"", "000050000", "000099900"} {
		var plan []byte
		if err := conn.QueryRow(e.ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT * FROM source.dwd_bzkscjzlb WHERE "ID" COLLATE "C">$1 ORDER BY "ID" COLLATE "C" LIMIT 500`, after).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(plan), "dwd_bzkscjzlb_cursor_c") || strings.Contains(string(plan), `"Node Type": "Sort"`) {
			t.Fatal("cursor not using ordered index", string(plan))
		}
		plans = append(plans, plan)
	}
	var plan []byte
	if err := conn.QueryRow(e.ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT * FROM source.dwd_bzkscjzlb WHERE "XH" COLLATE "C"=$1 AND "ID" COLLATE "C">$2 ORDER BY "ID" COLLATE "C" LIMIT 500`, "S0001", "").Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plan), "dwd_bzkscjzlb_student_c") {
		t.Fatal("student lookup not indexed", string(plan))
	}
	plans = append(plans, plan)
	if err := os.MkdirAll("../../artifacts/integration", 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.MarshalIndent(plans, "", "  ")
	if err := os.WriteFile("../../artifacts/integration/B03-explain.json", raw, 0600); err != nil {
		t.Fatal(err)
	}
	// A familiar name is insufficient: wrong column/collation must invalidate it.
	if _, err := conn.Exec(e.ctx, `DROP INDEX source.dwd_bzkscjzlb_cursor_c; CREATE INDEX dwd_bzkscjzlb_cursor_c ON source.dwd_bzkscjzlb ("XH")`); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckPrimaryIndex(e.ctx, h.Position, "dwd_bzkscjzlb"); err == nil {
		t.Fatal("wrong index accepted by name")
	}
}
