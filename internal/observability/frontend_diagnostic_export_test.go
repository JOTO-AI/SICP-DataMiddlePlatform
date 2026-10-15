package observability_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
)

// Opt-in export of actual persisted/read diagnostic DTOs from the existing failure tests.
// These are protocol failure scenarios, not normal business query mock records.
func exportFrontendDiagnostic(t *testing.T, row observability.FailedAttempt, db *logsDB, s *observability.Sanitizer) {
	t.Helper()
	dir := os.Getenv("SICP_DIAGNOSTIC_FIXTURE_DIR")
	if dir == "" {
		return
	}
	for _, body := range db.rows {
		safe, err := s.Restore([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if safe.Event().ID != row.EventID {
			continue
		}
		value := map[string]any{"provenance": t.Name(), "attempt": row, "envelope": json.RawMessage(safe.Bytes())}
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		name := strings.ReplaceAll(strings.SplitN(t.Name(), "/", 2)[1], " ", "_")
		if err := os.WriteFile(filepath.Join(dir, name+".json"), append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("export cannot find persisted attempt")
}
