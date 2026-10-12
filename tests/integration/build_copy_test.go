//go:build integration

package integration

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestI01_B05CopyMatchesInsertAndRejectsDuplicate(t *testing.T) {
	e := newEnvironment(t)
	e.migrate(t)
	position := storage.Position{Instance: storage.Main, Slot: 0}
	target := source.Target{Position: position, Generation: 1, CandidateID: "synthetic"}
	var rows []source.SourceRow
	for i, token := range []string{"3", "3.0", `"3"`, "null"} {
		rows = append(rows, source.SourceRow{"ID": json.RawMessage(`"00` + string(rune('1'+i)) + `"`), "XH": json.RawMessage(`"000123"`), "XF": json.RawMessage(token), "ETL_TIME": json.RawMessage(`"2026-09-01 12:34:56.123456"`)})
	}
	rows = append(rows, source.SourceRow{"ID": json.RawMessage(`"005"`)})
	var previous []source.SourceRow
	for _, copyMode := range []bool{false, true} {
		r := source.Repository{Registry: e.registry, Database: e.db, Gate: storageGate{}, UseCopy: copyMode}
		report, err := r.Rebuild(e.ctx, target, "dwd_bzkscjzlb", &rowStream{rows: rows}, 2)
		if err != nil || !report.Complete {
			t.Fatal(report, err)
		}
		got, err := r.ReadBatch(e.ctx, position, "dwd_bzkscjzlb", "", 100)
		if err != nil || len(got) != len(rows) {
			t.Fatal(err)
		}
		if copyMode && !reflect.DeepEqual(got, previous) {
			t.Fatal("COPY changed raw JSON, null, TEXT or timestamp")
		}
		previous = got
		for i, token := range []string{"3", "3.0", `"3"`, "null"} {
			if string(got[i]["XF"]) != token {
				t.Fatal("numeric lexeme changed")
			}
		}
		report, err = r.Rebuild(e.ctx, target, "dwd_bzkscjzlb", &rowStream{rows: []source.SourceRow{rows[0], rows[0]}}, 2)
		if err == nil || report.Complete {
			t.Fatal("duplicate key accepted")
		}
	}
}
