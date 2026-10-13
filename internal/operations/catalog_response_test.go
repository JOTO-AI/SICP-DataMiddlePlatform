package operations

import (
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"os"
	"strings"
	"testing"
)

// Frozen expectations were transcribed from the API field tables and the
// confirmed fourteen-table field plan, independently of Go DTOs/Catalog().
type reviewedCatalog struct {
	Objects   map[string]map[string]string `json:"objects"`
	Responses map[string]map[string]string `json:"responses"`
	Sources   map[string]struct {
		PrimaryKey string            `json:"primary_key"`
		Fields     map[string]string `json:"fields"`
	} `json:"sources"`
}

func reviewedFields(t *testing.T) reviewedCatalog {
	t.Helper()
	raw, err := os.ReadFile("testdata/reviewed_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var c reviewedCatalog
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func checkResponseFields(t *testing.T, f Field, want map[string]string, c reviewedCatalog) {
	t.Helper()
	if f.Type != "object" || len(f.Properties) != len(want) {
		t.Fatal("response field set changed", f, want)
	}
	for name, typ := range want {
		child, ok := f.Properties[name]
		if !ok || !child.Required {
			t.Fatal("response field omitted", name)
		}
		checkResponseType(t, child, typ, c)
	}
}
func checkResponseType(t *testing.T, f Field, typ string, c reviewedCatalog) {
	t.Helper()
	nullable := strings.HasSuffix(typ, "/null")
	typ = strings.TrimSuffix(typ, "/null")
	if f.Nullable != nullable {
		t.Fatal("null contract changed", typ, f)
	}
	if strings.HasSuffix(typ, "[]") {
		if f.Type != "array" || f.Items == nil {
			t.Fatal("missing array items")
		}
		checkResponseType(t, *f.Items, strings.TrimSuffix(typ, "[]"), c)
		return
	}
	if fields, ok := c.Objects[typ]; ok {
		checkResponseFields(t, f, fields, c)
		return
	}
	if f.Type != strings.ToLower(typ) {
		t.Fatal("field type changed", typ, f.Type)
	}
}
func TestT32_01And02AllBusinessResponseFieldsMatchIndependentContract(t *testing.T) {
	c := reviewedFields(t)
	ids := []string{"organizations.query", "students.query", "staff.query", "students.grades.query", "students.grades.batch-query", "students.timetable.query", "staff.timetable.query", "staff.teaching-classes.query", "teaching-classes.students.query"}
	for i, id := range ids {
		t.Run(id, func(t *testing.T) {
			api, ok := LookupAPI(id)
			if !ok {
				t.Fatal(id)
			}
			checkResponseFields(t, api.Response, c.Responses[string(rune('1'+i))], c)
		})
	}
}
func TestT32_01And02All274SourceFieldsAndExamplesMatchReviewedPlan(t *testing.T) {
	c := reviewedFields(t)
	if len(c.Sources) != 14 {
		t.Fatal("source set")
	}
	count := 0
	for id, source := range c.Sources {
		t.Run(id, func(t *testing.T) {
			api, ok := LookupAPI(id)
			if !ok || api.Path != "/apizt/openapi/invoke/sjzc/release/"+id || len(api.Response.Properties) != len(source.Fields) {
				t.Fatal("source contract", id)
			}
			record := map[string]any{}
			for name, typ := range source.Fields {
				count++
				f, ok := api.Response.Properties[name]
				if !ok {
					t.Fatal("missing field", name)
				}
				want := typ
				if typ == "timestamp" {
					want = "string"
					if !strings.Contains(f.Limit, "no timezone") {
						t.Fatal("timestamp timezone")
					}
				}
				if f.Type != want || f.Required != (name == source.PrimaryKey) || f.Nullable == (name == source.PrimaryKey) {
					t.Fatal("source field contract", name, f)
				}
				if name == source.PrimaryKey {
					record[name] = "synthetic-001"
				} else {
					record[name] = nil
				}
			}
			raw, _ := json.Marshal(record)
			if _, err := ingest.DecodeRecord(id, raw); err != nil {
				t.Fatal("documented null record rejected", err)
			}
			delete(record, source.PrimaryKey)
			raw, _ = json.Marshal(record)
			if _, err := ingest.DecodeRecord(id, raw); err == nil {
				t.Fatal("missing primary key accepted")
			}
			record = map[string]any{source.PrimaryKey: "synthetic-001"}
			raw, _ = json.Marshal(record)
			if _, err := ingest.DecodeRecord(id, raw); err != nil {
				t.Fatal("optional missing fields rejected", err)
			}
		})
	}
	if count != 274 {
		t.Fatal("field count", count)
	}
}
