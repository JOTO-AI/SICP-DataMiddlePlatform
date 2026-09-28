package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestT14_15RetiredFileDeletionIsConfinedAndRetryable(t *testing.T) {
	stagePath, targetPath := t.TempDir(), t.TempDir()
	stage, err := staging.OpenDirectory(stagePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Close() })
	target, err := staging.OpenDirectory(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	stageQuota, _ := staging.NewQuotaFiles(stage, 1<<20)
	targetQuota, _ := staging.NewQuotaFiles(target, 1<<20)
	m := candidate.Manifest{Combination: "Cold", Input: staging.SealRef{RunID: "old", SHA256: strings.Repeat("a", 64)}}
	for _, table := range source.Catalog() {
		m.Tables = append(m.Tables, candidate.TableVersion{Table: table.Name, Scope: "synthetic", SHA256: strings.Repeat("a", 64)})
	}
	// Model a previously committed file retirement interrupted before removal.
	raw, _ := json.Marshal(m.Input)
	if err := os.WriteFile(filepath.Join(stagePath, "old.retired.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"target-Cold.manifest.json", "target-Cold.table.jsonl", "target-Colder.manifest.json", "target-Ccurrent.manifest.json"} {
		if err := os.WriteFile(filepath.Join(targetPath, name), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files := retiredVersionFiles{staging: stageQuota, targets: targetQuota, manifest: m}
	r := control.Resource{ID: "version-Cold", Kind: "version", BundleID: "Cold"}
	if err := files.DeleteRetired(t.Context(), r); err == nil {
		t.Fatal("uncommitted retirement deleted files")
	}
	r.Retired = true
	wrong := r
	wrong.BundleID = "Ccurrent"
	if err := files.DeleteRetired(t.Context(), wrong); err == nil {
		t.Fatal("wrong resource deleted files")
	}
	wrongFiles := files
	wrongFiles.manifest.Input.RunID = "current"
	if err := wrongFiles.DeleteRetired(t.Context(), r); err == nil {
		t.Fatal("cross-task seal deleted")
	}
	for range 2 {
		if err := files.DeleteRetired(t.Context(), r); err != nil {
			t.Fatal("retirement retry", err)
		}
	}
	for _, name := range []string{"target-Cold.manifest.json", "target-Cold.table.jsonl"} {
		if _, err := os.Stat(filepath.Join(targetPath, name)); !os.IsNotExist(err) {
			t.Fatal("retired file retained", err)
		}
	}
	for _, name := range []string{"target-Colder.manifest.json", "target-Ccurrent.manifest.json"} {
		if _, err := os.Stat(filepath.Join(targetPath, name)); err != nil {
			t.Fatal("neighbor/current version removed", err)
		}
	}
}
