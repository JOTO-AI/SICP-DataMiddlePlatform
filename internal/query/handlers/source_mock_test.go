package handlers_test

import (
	"encoding/json"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit/querymock"
)

func originalMockQuery(t *testing.T) querymock.Query {
	t.Helper()
	w := newWorld()
	w.tables = make(map[string][]source.SourceRow)
	for table, rows := range querymock.Load(t, "../../..") {
		for _, raw := range rows {
			var row source.SourceRow
			if err := json.Unmarshal(raw, &row); err != nil {
				t.Fatal(err)
			}
			w.tables[table] = append(w.tables[table], row)
		}
	}
	h, _ := pipelineSetup(t, w)
	return func(op contract.Operation, body, token string) (map[string]any, string) {
		t.Helper()
		response := pipelineRequest(t.Context(), h, op, body, "synthetic-key", token)
		if response.Code != 200 {
			t.Fatalf("%s: %d %s", op, response.Code, response.Body.String())
		}
		return dataObject(t, response), response.Header().Get(snapshot.Header)
	}
}

func TestT25_08OriginalMockStudents(t *testing.T) {
	querymock.Students(t, originalMockQuery(t))
}

func TestT26_02OriginalMockOrganizations(t *testing.T) {
	querymock.Organizations(t, originalMockQuery(t))
}

func TestT26_03OriginalMockStaff(t *testing.T) {
	querymock.Staff(t, originalMockQuery(t))
}
