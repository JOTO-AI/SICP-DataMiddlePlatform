package candidate_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT12_04StorageValidationComparesEveryColumnWithoutLosingJSONPrecision(t *testing.T) {
	for _, kind := range []string{"valid", "stale-field", "missing-column", "missing-row", "json-number", "json-null"} {
		t.Run(kind, func(t *testing.T) {
			_, target, m, _, original, _ := buildFixture(t)
			table := source.Catalog()[0]
			jsonField := ""
			for _, column := range table.Columns {
				if column.Kind == value.JSON {
					jsonField = column.Name
					break
				}
			}
			if jsonField == "" {
				t.Fatal("fixture requires JSON column")
			}
			token := json.RawMessage(`9007199254740993`)
			if kind == "json-null" {
				token = json.RawMessage(`null`)
			}
			original.tables[table.Name][0].Fields[jsonField] = ingest.Token{Raw: token}
			m.Tables[0].SHA256, _ = merge.SourceDigest(t.Context(), table.Name, m.Tables[0].Scope, original.tables[table.Name])
			stored := &candidateSource{rows: map[string][]source.SourceRow{}}
			for _, row := range original.tables[table.Name] {
				projection := source.SourceRow{}
				for _, column := range table.Columns {
					token, present := row.Fields[column.Name]
					bound, err := value.Bind(column.Kind, value.Field{Present: present, Raw: token.Raw})
					if err != nil {
						t.Fatal(err)
					}
					raw, err := value.Restore(column.Kind, bound)
					if err != nil {
						t.Fatal(err)
					}
					projection[column.Name] = raw
				}
				stored.rows[table.Name] = append(stored.rows[table.Name], projection)
			}
			first := stored.rows[table.Name][0]
			switch kind {
			case "stale-field":
				for _, column := range table.Columns {
					if column.Kind == value.Text && column.Name != table.PrimaryKey {
						first[column.Name] = json.RawMessage(`"stale"`)
						break
					}
				}
			case "missing-column":
				delete(first, jsonField)
			case "missing-row":
				stored.rows[table.Name] = stored.rows[table.Name][:1]
			case "json-number":
				first[jsonField] = json.RawMessage(`9007199254740992`)
			case "json-null":
				first[jsonField] = nil
			}
			checker := candidate.StorageChecks{Target: original, Source: stored, Clock: testkit.FakeClock{Time: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}, Limits: read.DefaultLimits()}
			h := read.Handle{Position: target.Position, Generation: target.Generation, Combination: m.Combination}
			err := checker.SourceStorage(t.Context(), h, m)
			if (kind == "valid") != (err == nil) {
				t.Fatal(kind, err)
			}
			if original.opens != original.closed {
				t.Fatal("unclosed complete source")
			}
		})
	}
}
