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

func TestT04_05DirectoryQuotaChargesPendingAndPublishedFiles(t *testing.T) {
	d, err := staging.OpenDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	quota, err := staging.NewQuotaFiles(d, 5)
	if err != nil {
		t.Fatal(err)
	}
	temp, err := quota.WriteTemporary(context.Background(), []byte("123"))
	if err != nil {
		t.Fatal(err)
	}
	if err = quota.Publish(context.Background(), temp, "committed.data"); err != nil {
		t.Fatal(err)
	}
	if _, err = quota.WriteTemporary(context.Background(), []byte("45")); err != nil {
		t.Fatal(err)
	}
	if _, err = quota.WriteTemporary(context.Background(), []byte("6")); !errors.Is(err, staging.ErrCapacity) {
		t.Fatal("pending files uncharged")
	}
	if used, err := quota.Usage(context.Background()); err != nil || used != 5 {
		t.Fatal("wrong directory usage")
	}
}

type quotaProbe struct {
	*memoryFiles
	usageCalls int
	writeError error
}

func (f *quotaProbe) Usage(context.Context) (int64, error) { f.usageCalls++; return 512 << 20, nil }
func (f *quotaProbe) WriteTemporary(ctx context.Context, data []byte) (string, error) {
	if f.writeError != nil {
		return "", f.writeError
	}
	return f.memoryFiles.WriteTemporary(ctx, data)
}
func TestDisabledQuotaDoesNotScanOrCapVolumeAndPreservesWriteErrors(t *testing.T) {
	f := &quotaProbe{memoryFiles: newMemoryFiles()}
	q, err := staging.NewQuotaFiles(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staging.NewStore("unlimited", q, 1<<20); err != nil {
		t.Fatal("disabled quota rejected by store", err)
	}
	if _, err := q.WriteTemporary(context.Background(), []byte("next page")); err != nil {
		t.Fatal(err)
	}
	if f.usageCalls != 0 || f.writes != 1 {
		t.Fatal("unlimited write scanned or rejected the volume")
	}
	f.writeError = errors.New("synthetic disk full")
	if _, err := q.WriteTemporary(context.Background(), []byte("next page")); !errors.Is(err, f.writeError) {
		t.Fatal("filesystem failure hidden", err)
	}
	f.writeError = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.WriteTemporary(ctx, []byte("cancelled")); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if _, err := staging.NewQuotaFiles(f, -1); err == nil {
		t.Fatal("negative quota accepted")
	}
}
