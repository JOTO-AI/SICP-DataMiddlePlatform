package logquery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Project the exact persisted events exported by observability through the real log DTO.
func TestExportFrontendDiagnosticDetails(t *testing.T) {
	dir := os.Getenv("SICP_DIAGNOSTIC_FIXTURE_DIR")
	if dir == "" {
		t.Skip("opt-in frontend diagnostic export")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatal("no exported diagnostic scenarios", err)
	}
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(body, &value); err != nil {
			t.Fatal(err)
		}
		s, rec, _, _ := readFixture(t)
		rec.Event, err = s.Restore(value["envelope"])
		if err != nil {
			t.Fatal(err)
		}
		var attempt struct {
			LogSource Source `json:"log_source"`
		}
		if err := json.Unmarshal(value["attempt"], &attempt); err != nil {
			t.Fatal(err)
		}
		value["log_detail"], err = json.Marshal(project(attempt.LogSource, "epoch", rec))
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
