package candidate

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

func targetInput(t *testing.T, mode ingest.Mode, objects ...string) (*staging.Lease, TargetFiles, PrepareOptions, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	dir, err := staging.OpenDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	quota, err := staging.NewQuotaFiles(dir, 32<<20)
	if err != nil {
		t.Fatal(err)
	}
	store, err := staging.NewStore("acquire", quota, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	limits := ingest.PageLimits{MaxPages: 100, MaxRecords: 1000, MaxBytes: 1 << 20}
	for _, d := range ingest.Catalog() {
		var rows []ingest.Record
		if d.Name == "dwd_xqjbsjzlb" {
			for _, object := range objects {
				row, err := ingest.DecodeRecord(d.Name, []byte(object))
				if err != nil {
					t.Fatal(err)
				}
				rows = append(rows, row)
			}
		}
		p, err := ingest.NewPager(d.Name, 100, limits)
		if err != nil {
			t.Fatal(err)
		}
		page := ingest.Page{Total: int64(len(rows)), Records: rows}
		if err := p.Accept(1, page); err != nil {
			t.Fatal(err)
		}
		request := ingest.PageRequest{Table: d.Name, Mode: mode, Page: 1, PageSize: 100}
		if mode == ingest.Incremental {
			request.StartTime = "2026-09-01 00:00:00"
			request.EndTime = "2026-09-01 01:00:00"
		}
		if err := store.CommitPage(ctx, request, page, p.Manifest().Pages[0]); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := store.Seal(ctx, limits)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Retain(ctx, ref, "initializer")
	if err != nil {
		t.Fatal(err)
	}
	files := TargetFiles{Files: quota, MaxChunkBytes: 512, MaxChunks: 1000}
	o := PrepareOptions{Combination: "C1", Scope: "mock-school", Mode: mode, SpoolDirectory: t.TempDir(), Limits: merge.Limits{BatchRows: 2, BatchBytes: 256, FanIn: 2, MaxRuns: 1000, MaxSpoolBytes: 16 << 20, InputPageRows: 100, InputPageBytes: 1 << 20}}
	return lease, files, o, root
}

func targetObjects(t *testing.T, f TargetFiles, m Manifest) []string {
	t.Helper()
	for _, table := range m.Tables {
		if table.Table != "dwd_xqjbsjzlb" {
			continue
		}
		r, err := f.Open(context.Background(), m, table)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close(context.Background())
		var out []string
		for {
			row, err := r.Next(context.Background())
			if errors.Is(err, io.EOF) {
				return out
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := row.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, string(raw))
		}
	}
	t.Fatal("campus absent")
	return nil
}

func TestT12_11ImmutableCompleteTargetFeedsBothCopiesAndDetectsCorruption(t *testing.T) {
	lease, f, o, root := targetInput(t, ingest.Full, `{"XQH":"c","XQMC":null}`, `{"XQH":"a"}`, `{"XQH":"b","XQMC":9007199254740993}`)
	ctx := context.Background()
	m, err := f.Prepare(ctx, o, lease)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`{"XQH":"a"}`, `{"XQH":"b","XQMC":9007199254740993}`, `{"XQH":"c","XQMC":null}`}
	for range 2 {
		if got := targetObjects(t, f, m); !reflect.DeepEqual(got, want) {
			t.Fatalf("original source changed: %v", got)
		}
	}
	if again, err := f.Prepare(ctx, o, lease); err != nil || !reflect.DeepEqual(m, again) {
		t.Fatalf("immutable retry failed: %v", err)
	}
	doc, err := f.document(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range doc.Tables {
		if len(table.Chunks) == 0 {
			continue
		}
		if err := os.WriteFile(filepath.Join(root, table.Chunks[0].Name), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		break
	}
	if err := f.Validate(ctx, m); err == nil {
		t.Fatal("corrupt source accepted")
	}
}

func TestT12_11IncrementalTargetIncludesUnchangedOriginalRows(t *testing.T) {
	lease, baseFiles, options, _ := targetInput(t, ingest.Full, `{"XQH":"a"}`, `{"XQH":"b","XQMC":"before"}`)
	ctx := context.Background()
	base, err := baseFiles.Prepare(ctx, options, lease)
	if err != nil {
		t.Fatal(err)
	}
	delta, f, o, _ := targetInput(t, ingest.Incremental, `{"XQH":"b","XQMC":"after"}`)
	o.Combination = "C2"
	o.Base = &base
	o.BaseSource = baseFiles
	m, err := f.Prepare(ctx, o, delta)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`{"XQH":"a"}`, `{"XQH":"b","XQMC":"after"}`}
	if got := targetObjects(t, f, m); !reflect.DeepEqual(got, want) {
		t.Fatalf("unchanged baseline lost: %v", got)
	}
}

func TestT12_11TargetCapacityAndInputModeFailureLeaveNoCompleteManifest(t *testing.T) {
	for _, failure := range []string{"chunks", "bytes", "mode"} {
		t.Run(failure, func(t *testing.T) {
			lease, f, o, root := targetInput(t, ingest.Full, `{"XQH":"a"}`, `{"XQH":"b"}`, `{"XQH":"c"}`)
			switch failure {
			case "chunks":
				f.MaxChunks = 1
			case "bytes":
				f.MaxChunkBytes = 32
			case "mode":
				o.Mode = ingest.ForceFull
			}
			if _, err := f.Prepare(context.Background(), o, lease); err == nil {
				t.Fatal("incomplete target accepted")
			}
			if _, err := os.Stat(filepath.Join(root, manifestName(o.Combination))); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed target has manifest")
			}
		})
	}
}
