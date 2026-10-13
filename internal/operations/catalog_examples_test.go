package operations

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func TestT32_02And04BusinessExamplesUseRealParserAndNonNullArrays(t *testing.T) {
	for _, api := range Catalog() {
		if api.Kind != "business" {
			continue
		}
		t.Run(api.ID, func(t *testing.T) {
			raw, e := json.Marshal(api.RequestExample)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = transport.Parse(contract.Operation(api.ID), bytes.NewReader(raw)); e != nil {
				t.Fatal("published example violates real request contract", e)
			}
			data := api.SuccessExample.(map[string]any)["data"].(map[string]any)
			if _, e = time.Parse(time.RFC3339, data["synced_at"].(string)); e != nil {
				t.Fatal("invalid example sync time")
			}
			if items, ok := data["items"].([]any); !ok || len(items) != 0 {
				t.Fatal("empty success is not an array")
			}
			for name, field := range api.Request.Properties {
				if field.Required {
					if _, present := api.RequestExample.(map[string]any)[name]; !present {
						t.Fatal("required field absent", name)
					}
				}
			}
		})
	}
}
