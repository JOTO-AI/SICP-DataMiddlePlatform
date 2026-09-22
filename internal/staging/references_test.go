package staging_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

func (f *memoryFiles) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.failure == "remove" {
		f.failure = ""
		return errors.New("interrupted removal")
	}
	delete(f.files, name)
	return nil
}

func TestT04_15SecondBuildAndRecoveryReferencesSurviveReconstruction(t *testing.T) {
	s, files, ref := completeStore(t)
	var leases []*staging.Lease
	for _, owner := range []string{"build-m", "build-s", "recovery"} {
		l, err := s.Retain(context.Background(), ref, owner)
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, l)
	}
	if err := leases[0].Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	// New components observe durable pins without process-local state.
	reopened := store(t, files)
	if err := reopened.Cleanup(context.Background(), ref); !errors.Is(err, staging.ErrReferenced) {
		t.Fatal("second build/recovery input deleted")
	}
	second, err := reopened.Retain(context.Background(), ref, "build-s")
	if err != nil {
		t.Fatal(err)
	}
	if second.Reference() != ref {
		t.Fatal("second build uses different input")
	}
	if _, err := second.ReadPage(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if err := second.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Cleanup(context.Background(), ref); !errors.Is(err, staging.ErrReferenced) {
		t.Fatal("recovery input deleted")
	}
	recovery, err := reopened.Retain(context.Background(), ref, "recovery")
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Cleanup(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Retain(context.Background(), ref, "late-build"); !errors.Is(err, staging.ErrRetired) {
		t.Fatal("retired source retained")
	}
	if _, err := reopened.OpenSealed(context.Background(), ref); !errors.Is(err, staging.ErrRetired) {
		t.Fatal("retired input reopened")
	}
	if len(files.files) != 1 || files.files["run-001.retired.json"] == nil {
		t.Fatal("cleanup omitted files or retirement marker")
	}
}

func TestT04_15ReferenceRetryCannotChangeIdentityOrResurrectReleasedOwner(t *testing.T) {
	s, files, ref := completeStore(t)
	l, err := s.Retain(context.Background(), ref, "build-s")
	if err != nil {
		t.Fatal(err)
	}
	writes := files.writes
	if _, err := s.Retain(context.Background(), ref, "build-s"); err != nil || files.writes != writes {
		t.Fatal("retain replay created duplicate fact")
	}
	wrong := ref
	wrong.SHA256 = strings.Repeat("0", 64)
	if _, err := s.Retain(context.Background(), wrong, "build-s"); err == nil {
		t.Fatal("reference rebound")
	}
	if err := l.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	writes = files.writes
	if err := l.Release(context.Background()); err != nil || files.writes != writes {
		t.Fatal("release retry not idempotent")
	}
	if _, err := s.Retain(context.Background(), ref, "build-s"); !errors.Is(err, staging.ErrReleased) {
		t.Fatal("released owner resurrected")
	}
	if _, err := l.ReadPage(context.Background(), 0); !errors.Is(err, staging.ErrReleased) {
		t.Fatal("released handle still read")
	}
}

func TestT04_15InterruptedCleanupRetiresBeforeDeletionAndResumes(t *testing.T) {
	s, files, ref := completeStore(t)
	files.failure = "remove"
	if err := s.Cleanup(context.Background(), ref); err == nil {
		t.Fatal("injected deletion failure ignored")
	}
	if _, err := s.Retain(context.Background(), ref, "late"); !errors.Is(err, staging.ErrRetired) {
		t.Fatal("new pin entered partially deleted input")
	}
	r, p, m := stagedPage(t)
	if err := s.CommitPage(context.Background(), r, p, m); !errors.Is(err, staging.ErrRetired) {
		t.Fatal("retired run accepted writes")
	}
	if _, err := s.Seal(context.Background(), sealLimits()); !errors.Is(err, staging.ErrRetired) {
		t.Fatal("retired run resealed")
	}
	reopened := store(t, files)
	if err := reopened.Cleanup(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Cleanup(context.Background(), ref); err != nil {
		t.Fatal("cleanup retry failed")
	}
}

func TestT04_15CleanupRetryConfirmsRetirementDurabilityBeforeAnyRemoval(t *testing.T) {
	s, files, ref := completeStore(t)
	files.failure = "ack_data" // Marker publication is visible but acknowledgement failed.
	if err := s.Cleanup(context.Background(), ref); err == nil {
		t.Fatal("publication failure ignored")
	}
	if files.files["run-001.retired.json"] == nil {
		t.Fatal("fault did not leave visible marker")
	}
	before := len(files.files)
	files.failure = "sync"
	if err := s.Cleanup(context.Background(), ref); err == nil {
		t.Fatal("retry ignored retirement sync failure")
	}
	if len(files.files) != before {
		t.Fatal("deleted before retirement durability was confirmed")
	}
	files.failure = ""
	if err := s.Cleanup(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
}

func TestT04_15RetainAndCleanupSerializeOnSharedVolume(t *testing.T) {
	for i := 0; i < 20; i++ {
		s, files, ref := completeStore(t)
		// Both Stores use the same quota, as required for concurrent access.
		quota, err := staging.NewQuotaFiles(files, 128<<20)
		if err != nil {
			t.Fatal(err)
		}
		a, err := staging.NewStore("run-001", quota, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		b, err := staging.NewStore("run-001", quota, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		_ = s
		var pin *staging.Lease
		var retainErr, cleanErr error
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; pin, retainErr = a.Retain(context.Background(), ref, "build-s") }()
		go func() { defer wg.Done(); <-start; cleanErr = b.Cleanup(context.Background(), ref) }()
		close(start)
		wg.Wait()
		if retainErr == nil {
			if !errors.Is(cleanErr, staging.ErrReferenced) {
				t.Fatal("cleanup crossed live reference")
			}
			if _, err := pin.ReadPage(context.Background(), 0); err != nil {
				t.Fatal(err)
			}
		} else if cleanErr != nil || !errors.Is(retainErr, staging.ErrRetired) {
			t.Fatalf("unexpected serialization: %v %v", retainErr, cleanErr)
		}
	}
}

func TestT04_15DirectoryRetirementPersistsAfterReopen(t *testing.T) {
	root := t.TempDir()
	d, err := staging.OpenDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	s := store(t, d)
	for _, definition := range ingest.Catalog() {
		commitEmpty(t, s, definition.Name)
	}
	ref, err := s.Seal(context.Background(), sealLimits())
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.Retain(context.Background(), ref, "recovery")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = staging.OpenDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s = store(t, d)
	if err := s.Cleanup(context.Background(), ref); !errors.Is(err, staging.ErrReferenced) {
		t.Fatal("durable pin lost on reopen")
	}
	l, err = s.Retain(context.Background(), ref, "recovery")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read(context.Background(), "run-001.sealed.json", 1<<20); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("sealed file not removed")
	}
}

type boundedReads struct {
	*memoryFiles
	track               bool
	largestPayloadLimit int64
}

func (f *boundedReads) Read(ctx context.Context, name string, limit int64) ([]byte, error) {
	if f.track && strings.HasSuffix(name, ".jsonl") && limit > f.largestPayloadLimit {
		f.largestPayloadLimit = limit
	}
	return f.memoryFiles.Read(ctx, name, limit)
}

func TestT04_14BoundedLeaseEnforcesPageLimitDuringValidationAndLaterReads(t *testing.T) {
	files := &boundedReads{memoryFiles: newMemoryFiles()}
	s := store(t, files)
	for _, d := range ingest.Catalog() {
		if d.Name == "dwd_xqjbsjzlb" {
			r, p, m := stagedPage(t)
			if err := s.CommitPage(context.Background(), r, p, m); err != nil {
				t.Fatal(err)
			}
		} else {
			commitEmpty(t, s, d.Name)
		}
	}
	ref, err := s.Seal(context.Background(), sealLimits())
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.Retain(context.Background(), ref, "build-m")
	if err != nil {
		t.Fatal(err)
	}
	files.track = true
	bounded, err := l.Bounded(context.Background(), 64, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i, entry := range bounded.Pages() {
		if _, err := bounded.ReadPage(context.Background(), i); err != nil {
			t.Fatal(err)
		}
		if entry.Request.Table == "dwd_xqjbsjzlb" {
			files.files[entry.Payload] = []byte(strings.Repeat("x", 65))
		}
	}
	if _, err := l.Bounded(context.Background(), 64, 2); !errors.Is(err, staging.ErrCorrupt) {
		t.Fatal("oversized corrupt page not rejected during bounded verification")
	}
	if files.largestPayloadLimit != 64 {
		t.Fatalf("page read bypassed stream cap: %d", files.largestPayloadLimit)
	}
}
