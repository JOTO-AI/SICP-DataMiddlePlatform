//go:build linux

package assembly

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

func TestWorkerRestartWithExistingDataAndOptionalQuotas(t *testing.T) {
	c := config.Runtime{
		Service:      config.Config{Role: config.Worker},
		LogDirectory: t.TempDir(), LogBytes: 1 << 20, LogFiles: 100,
		StagingDirectory: t.TempDir(), TargetDirectory: t.TempDir(), SpoolDirectory: t.TempDir(),
	}
	for _, path := range []string{c.StagingDirectory, c.TargetDirectory} {
		if err := os.WriteFile(filepath.Join(path, "retained"), []byte("existing data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name            string
		staging, target int64
		wantCapacity    bool
	}{
		{"unlimited", 0, 0, false},
		{"staging quota exceeded", 1, 0, true},
		{"target quota exceeded", 0, 1, true},
		{"unlimited after restart", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.StagingBytes, c.TargetBytes = tc.staging, tc.target
			files, err := openFiles(t.Context(), c)
			if files != nil {
				defer func() {
					if err := files.close(context.Background()); err != nil {
						t.Error(err)
					}
				}()
			}
			if tc.wantCapacity {
				if !errors.Is(err, staging.ErrCapacity) {
					t.Fatalf("expected capacity error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, quota := range []*staging.QuotaFiles{files.Staging, files.Targets} {
				if _, err := quota.WriteTemporary(t.Context(), []byte("new data")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
