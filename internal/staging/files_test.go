package staging_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

func TestT04_01DirectoryPublishesImmutableFilesWithinRoot(t *testing.T) {
	root := t.TempDir()
	d, err := staging.OpenDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	temp, err := d.WriteTemporary(context.Background(), []byte("complete"))
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Publish(context.Background(), temp, "page.data"); err != nil {
		t.Fatal(err)
	}
	temp, err = d.WriteTemporary(context.Background(), []byte("different"))
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Publish(context.Background(), temp, "page.data"); !errors.Is(err, fs.ErrExist) {
		t.Fatal("immutable destination overwritten")
	}
	data, err := d.Read(context.Background(), "page.data", 100)
	if err != nil || string(data) != "complete" {
		t.Fatal("published file changed")
	}
	if _, err = d.Read(context.Background(), "../escape", 100); err == nil {
		t.Fatal("escaped root")
	}
	if _, err = d.Read(context.Background(), "page.data", 1); !errors.Is(err, staging.ErrCapacity) {
		t.Fatal("oversize read accepted")
	}
	outside := t.TempDir()
	if err = os.WriteFile(filepath.Join(outside, "secret"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Read(context.Background(), "link", 100); err == nil {
		t.Fatal("symlink escaped root")
	}
}

func TestT04_02DirectoryStoreSurvivesReopen(t *testing.T) {
	root := t.TempDir()
	d, err := staging.OpenDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	s := store(t, d)
	r, p, m := stagedPage(t)
	if err = s.CommitPage(context.Background(), r, p, m); err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = staging.OpenDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s = store(t, d)
	if err = s.CommitPage(context.Background(), r, p, m); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries(context.Background())
	if err != nil || len(entries) != 1 || entries[0].Summary.Count != 2 {
		t.Fatal("reopened store lost or duplicated receipt")
	}
}
