//go:build integration && linux

package integration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func targetExists(t *testing.T, f *runtimeFixture, combination string) bool {
	t.Helper()
	names, err := f.wr.Files.Targets.List(f.e.ctx, "target-"+combination+".")
	if err != nil {
		t.Fatal(err)
	}
	return len(names) > 0
}

func TestI05_08(t *testing.T) {
	for _, reference := range []string{"pagination", "recovery"} {
		t.Run(reference, func(t *testing.T) {
			f := newRuntime(t, true)
			_, manifest := loadBundle(t, f, "Cinit")
			files, err := staging.NewStore("init", f.wr.Files.Staging, 64<<20)
			if err != nil {
				t.Fatal(err)
			}
			var token string
			var lease *staging.Lease
			if reference == "pagination" {
				_, token = f.queryOK(t, contract.StudentGrades, allGrades, "")
			} else {
				if err := f.wr.Control.ResourceReference(f.e.ctx, "version-Cinit", "recovery-reader", "recovery", false); err != nil {
					t.Fatal(err)
				}
				lease, err = files.Retain(f.e.ctx, manifest.Input, "recovery-reader")
				if err != nil {
					t.Fatal(err)
				}
			}
			changedGrade(f)
			f.run(t, "force_full", "next")
			if !targetExists(t, f, "Cinit") || countControl(t, f, "SELECT count(*) FROM control.resources WHERE id='version-Cinit' AND retired") != 0 {
				t.Fatal("referenced version was retired or removed")
			}
			if _, err := files.OpenSealed(f.e.ctx, manifest.Input); err != nil {
				t.Fatal("referenced source seal removed", err)
			}
			if token != "" {
				data, _ := f.queryOK(t, contract.StudentGrades, allGrades, token)
				if data["items"].([]any)[1].(map[string]any)["score"] != float64(90) {
					t.Fatal("old snapshot data changed")
				}
			}
			admitTask(t, f, "force_full", "third")
			if err := f.worker.Engine.Cycle(f.e.ctx); !errors.Is(err, control.ErrBusy) {
				t.Fatal("referenced old slot was reused", err)
			}
			if countControl(t, f, "SELECT count(*) FROM control.slots WHERE bundle_id='Cinit' AND generation=1") != 2 || !targetExists(t, f, "Cinit") {
				t.Fatal("blocked slot reuse changed physical identity or files")
			}
			requests := sourceRequests(f)
			if reference == "pagination" {
				if _, err := f.e.connection(t, 4).Exec(f.e.ctx, "UPDATE control.query_snapshots SET expires_at=created_at+interval '1 microsecond' WHERE bundle_id='Cinit'"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := lease.Release(f.e.ctx); err != nil {
					t.Fatal(err)
				}
				if err := f.wr.Control.ResourceReference(f.e.ctx, "version-Cinit", "recovery-reader", "recovery", true); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.worker.Engine.Cycle(f.e.ctx); err != nil {
				t.Fatal("resume after reference release", err)
			}
			if loadTask(t, f, "third").Status != "success_published" || sourceRequests(f) != requests || targetExists(t, f, "Cinit") || !targetExists(t, f, "Cthird") {
				t.Fatal("release did not permit safe reuse and bounded cleanup")
			}
			if _, err := files.OpenSealed(f.e.ctx, manifest.Input); !errors.Is(err, staging.ErrRetired) {
				t.Fatal("old source files not retired", err)
			}
			if countControl(t, f, "SELECT count(*) FROM control.steps WHERE task_id='third' AND name='maintenance/version-Cinit' AND status='succeeded'") != 1 {
				t.Fatal("successful cleanup not recorded")
			}
		})
	}
}

func TestI05_09(t *testing.T) {
	f := newRuntime(t, true)
	// Both candidates are built before publication. Remove directory write
	// permission at Redis application so only post-publication cleanup fails.
	t.Cleanup(func() {
		if err := os.Chmod(f.wc.TargetDirectory, 0700); err != nil {
			t.Error(err)
		}
	})
	var injected bool
	f.redis.afterDo = func(command string, args []any, value any, err error) (any, error) {
		if command == "EVAL" && strings.Contains(args[4].(string), `"event_id":"next-publish"`) && err == nil {
			if e := os.Chmod(f.wc.TargetDirectory, 0500); e != nil {
				return nil, e
			}
			injected = true
		}
		return value, err
	}
	changedGrade(f)
	f.run(t, "force_full", "next")
	f.redis.afterDo = nil
	completed := loadTask(t, f, "next")
	active := activeMetadata(t, f)
	if !injected || completed.Status != "success_published" || completed.PublishedAt == nil || active.Position.BundleID != "Cnext" || !targetExists(t, f, "Cinit") {
		t.Fatal("post-publication filesystem failure not isolated")
	}
	if countControl(t, f, "SELECT count(*) FROM control.steps WHERE task_id='next' AND name='maintenance/version-Cinit' AND status='failed' AND evidence->>'result'='cleanup_failed'") != 1 || countControl(t, f, "SELECT count(*) FROM control.resources WHERE id='version-Cinit' AND retired") != 1 {
		t.Fatal("cleanup failure or committed retirement not recorded")
	}
	if _, err := os.Stat(filepath.Join(f.wc.TargetDirectory, "target-Cnext.manifest.json")); err != nil {
		t.Fatal("current target damaged", err)
	}
	assertProgress(t, f, "next")
	data, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
	if data["items"].([]any)[1].(map[string]any)["score"] != float64(91) {
		t.Fatal("current version stopped serving after cleanup failure")
	}
	if err := os.Chmod(f.wc.TargetDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	f.run(t, "incremental", "retry-cleanup")
	if targetExists(t, f, "Cinit") || !targetExists(t, f, "Cnext") || activeMetadata(t, f) != active || countControl(t, f, "SELECT count(*) FROM control.steps WHERE task_id='retry-cleanup' AND name='maintenance/version-Cinit' AND status='succeeded'") != 1 {
		t.Fatal("cleanup retry changed publication or failed to finish")
	}
	if got := loadTask(t, f, "next"); got.Status != completed.Status || !got.PublishedAt.Equal(*completed.PublishedAt) || !got.FinishedAt.Equal(*completed.FinishedAt) {
		t.Fatal("cleanup retry rewrote successful task")
	}
	if ttl, err := f.redis.raw.Do(f.e.ctx, "TTL", f.redis.prefix+publication.ActiveKey); err != nil || ttl != int64(-1) {
		t.Fatal("active metadata expired during maintenance", ttl, err)
	}
}
