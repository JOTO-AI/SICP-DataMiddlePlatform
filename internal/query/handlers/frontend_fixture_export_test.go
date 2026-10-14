package handlers_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit/querymock"
)

// This opt-in exporter records frontend fixtures through the real HTTP pipeline.
// It does not run the paused T/OI suite, alter normal source rows, or contact I/O.
// Captures are renderer inputs, not independent business correctness assertions.
func TestFrontendFixtureExport(t *testing.T) {
	output := os.Getenv("SICP_FRONTEND_FIXTURE_OUTPUT")
	if output == "" {
		t.Skip("set SICP_FRONTEND_FIXTURE_OUTPUT to export frontend fixtures")
	}
	type capture struct {
		Operation       contract.Operation `json:"operation"`
		Scenario        string             `json:"scenario"`
		Request         json.RawMessage    `json:"request"`
		RequestSnapshot string             `json:"request_snapshot,omitempty"`
		Status          int                `json:"status"`
		Headers         map[string]string  `json:"headers"`
		Response        json.RawMessage    `json:"response"`
	}
	requests := map[contract.Operation]string{
		contract.StudentGrades:         `{"student_number":"00202600001","academic_year":"all"}`,
		contract.BatchGrades:           `{"academic_year":"all"}`,
		contract.StudentTimetable:      `{"student_number":"00202600001","academic_year":"2026-2027","semester":"1"}`,
		contract.StaffTimetable:        `{"staff_number":"001","academic_year":"2026-2027","semester":"1"}`,
		contract.Organizations:         `{}`,
		contract.Students:              `{}`,
		contract.Staff:                 `{}`,
		contract.StaffTeachingClasses:  `{"staff_number":"001","academic_year":"2026-2027","semester":"1"}`,
		contract.TeachingClassStudents: `{"teaching_class_number":"J2026-0001","planned_course_code":"000C0001","academic_year":"2026-2027","semester":"1"}`,
	}
	var captures []capture
	for _, definition := range contract.Definitions() {
		t.Run(string(definition.Operation), func(t *testing.T) {
			world := newWorld()
			world.tables = make(map[string][]source.SourceRow)
			for table, rows := range querymock.Load(t, "../../..") {
				for _, raw := range rows {
					var row source.SourceRow
					if err := json.Unmarshal(raw, &row); err != nil {
						t.Fatal(err)
					}
					world.tables[table] = append(world.tables[table], row)
				}
			}
			handler, _ := pipelineSetup(t, world)
			body := requests[definition.Operation]
			if body == "" {
				t.Fatal("operation missing a source-backed request")
			}
			run := func(scenario, body, token string, status int, wantItems bool) string {
				response := pipelineRequest(t.Context(), handler, definition.Operation, body, "synthetic-key", token)
				if response.Code != status {
					t.Fatalf("%s: status %d: %s", scenario, response.Code, response.Body.String())
				}
				if status == 200 {
					var envelope struct {
						Data struct {
							Items []json.RawMessage `json:"items"`
						} `json:"data"`
					}
					if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
						t.Fatal(err)
					}
					if (len(envelope.Data.Items) > 0) != wantItems {
						t.Fatalf("%s: unexpected item count %d", scenario, len(envelope.Data.Items))
					}
				}
				headers := map[string]string{}
				for _, key := range []string{"X-Request-ID", "X-Query-Snapshot", "X-SICP-Combination", "Content-Type", "Retry-After"} {
					if value := response.Header().Get(key); value != "" {
						headers[key] = value
					}
				}
				captures = append(captures, capture{definition.Operation, scenario, json.RawMessage(body), token, response.Code, headers, append(json.RawMessage(nil), response.Body.Bytes()...)})
				return response.Header().Get(snapshot.Header)
			}
			token := run("success", body, "", 200, true)
			var empty map[string]any
			if err := json.Unmarshal([]byte(body), &empty); err != nil {
				t.Fatal(err)
			}
			if contract.SupportsSnapshot(definition.Operation) {
				empty["page"] = 1000000
			} else {
				empty["semester"] = "2"
			}
			emptyJSON, err := json.Marshal(empty)
			if err != nil {
				t.Fatal(err)
			}
			run("empty", string(emptyJSON), "", 200, false)
			run("invalid_argument", `{"unexpected":true}`, "", 400, false)
			if contract.SupportsSnapshot(definition.Operation) {
				if token == "" {
					t.Fatal("missing snapshot")
				}
				// Invalidate the actual captured snapshot through the existing world.
				world.mu.Lock()
				world.readable[world.active] = false
				world.mu.Unlock()
				run("snapshot_expired", body, token, 409, false)
			}
		})
	}
	if t.Failed() {
		return
	}
	document := struct {
		Version  int              `json:"version"`
		Source   string           `json:"source"`
		Captures []capture        `json:"captures"`
		Catalog  []operations.API `json:"catalog"`
	}{1, "mock_data/snapshots/initial; real backend pipeline with existing in-memory I/O adapters", captures, operations.Catalog()}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	temporary, err := os.CreateTemp(output, ".business-*.json")
	if err != nil {
		t.Fatal(err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		temporary.Close()
		t.Fatal(err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(name, filepath.Join(output, "business.json")); err != nil {
		t.Fatal(err)
	}
}

// TestFrontendPaginationFixtureExport records actual same-snapshot page chains.
// The existing baseline provides requests; normal rows still come from querymock.
func TestFrontendPaginationFixtureExport(t *testing.T) {
	output := os.Getenv("SICP_FRONTEND_PAGINATION_OUTPUT")
	if output == "" {
		t.Skip("set SICP_FRONTEND_PAGINATION_OUTPUT to export pagination fixtures")
	}
	type capture struct {
		Operation       contract.Operation `json:"operation"`
		Scenario        string             `json:"scenario"`
		Request         json.RawMessage    `json:"request"`
		RequestSnapshot string             `json:"request_snapshot,omitempty"`
		Status          int                `json:"status"`
		Headers         map[string]string  `json:"headers"`
		Response        json.RawMessage    `json:"response"`
	}
	var baseline struct {
		Captures []capture `json:"captures"`
	}
	raw, err := os.ReadFile(filepath.Join(output, "business.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	var captures []capture
	for _, sample := range baseline.Captures {
		if sample.Scenario != "success" || !contract.SupportsSnapshot(sample.Operation) {
			continue
		}
		t.Run(string(sample.Operation), func(t *testing.T) {
			world := newWorld()
			world.tables = make(map[string][]source.SourceRow)
			for table, rows := range querymock.Load(t, "../../..") {
				for _, raw := range rows {
					var row source.SourceRow
					if err := json.Unmarshal(raw, &row); err != nil {
						t.Fatal(err)
					}
					world.tables[table] = append(world.tables[table], row)
				}
			}
			handler, _ := pipelineSetup(t, world)
			var body map[string]any
			if err := json.Unmarshal(sample.Request, &body); err != nil {
				t.Fatal(err)
			}
			body["page"], body["page_size"] = 1, 1
			run := func(scenario, token string, status int) (string, int) {
				payload, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				response := pipelineRequest(t.Context(), handler, sample.Operation, string(payload), "synthetic-key", token)
				if response.Code != status {
					t.Fatalf("%s: HTTP %d: %s", scenario, response.Code, response.Body.String())
				}
				var envelope struct {
					Data struct {
						Total int               `json:"total"`
						Items []json.RawMessage `json:"items"`
					} `json:"data"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if status == 200 && len(envelope.Data.Items) != 1 {
					t.Fatalf("%s: expected one actual record", scenario)
				}
				headers := map[string]string{}
				for _, key := range []string{"X-Request-ID", "X-Query-Snapshot", "X-SICP-Combination", "Content-Type"} {
					if value := response.Header().Get(key); value != "" {
						headers[key] = value
					}
				}
				captures = append(captures, capture{sample.Operation, scenario, payload, token, response.Code, headers, append(json.RawMessage(nil), response.Body.Bytes()...)})
				return response.Header().Get(snapshot.Header), envelope.Data.Total
			}
			token, total := run("page_1", "", 200)
			if total < 2 || total > 1000 || token == "" {
				t.Fatalf("unexpected fixture page count or token: %d", total)
			}
			for page := 2; page <= total; page++ {
				body["page"] = page
				next, _ := run("page_"+strconv.Itoa(page), token, 200)
				if next != token {
					t.Fatal("snapshot changed while paging")
				}
			}
			body["page"], body["page_size"] = 2, 2
			run("snapshot_conflict", token, 409)
			body["page_size"] = 1
			world.mu.Lock()
			world.readable[world.active] = false
			world.mu.Unlock()
			run("snapshot_expired", token, 409)
		})
	}
	if t.Failed() {
		return
	}
	document := struct {
		Version  int       `json:"version"`
		Source   string    `json:"source"`
		Captures []capture `json:"captures"`
	}{1, "existing business requests and mock_data/snapshots/initial; real in-memory backend pipeline", captures}
	raw, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "pagination.json"), append(raw, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}
