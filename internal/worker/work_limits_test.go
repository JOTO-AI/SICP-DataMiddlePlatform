package worker

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

func TestT15_09RuntimeFullBatchFitsPersistedTargetChunk(t *testing.T) {
	target, options := targetPreparation(config.Runtime{SourceScope: "synthetic", SpoolBytes: 64 << 20}, nil, "Csynthetic")
	// Exercise the boundary where every permitted row contributes its JSONL
	// delimiter in addition to a completely filled canonical-payload budget.
	var chunk bytes.Buffer
	payload := bytes.Repeat([]byte{'x'}, int(options.Limits.BatchBytes))
	chunk.Write(payload)
	for range options.Limits.BatchRows {
		chunk.WriteByte('\n')
	}
	if int64(chunk.Len()) > target.MaxChunkBytes {
		t.Fatalf("full merge batch requires %d bytes; target accepts %d", chunk.Len(), target.MaxChunkBytes)
	}
	if options.Limits.BatchBytes <= 0 || options.Limits.BatchRows <= 0 {
		t.Fatal("empty runtime merge budget")
	}
}

func TestLargeSourcePageRemainsReadableAfterPageSizeIsReduced(t *testing.T) {
	ctx := context.Background()
	dir, err := staging.OpenDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	quota, err := staging.NewQuotaFiles(dir, 32<<20)
	if err != nil {
		t.Fatal(err)
	}
	store, err := staging.NewStore("large-page", quota, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	limits := ingest.PageLimits{MaxPages: 100, MaxRecords: 10000, MaxBytes: 1 << 20}
	for _, table := range ingest.Catalog() {
		var rows []ingest.Record
		if table.Name == "dwd_bzkscjzlb" {
			for i := 0; i < 1001; i++ {
				r, err := ingest.DecodeRecord(table.Name, []byte(fmt.Sprintf(`{"ID":"grade-%04d"}`, i)))
				if err != nil {
					t.Fatal(err)
				}
				rows = append(rows, r)
			}
		}
		pager, err := ingest.NewPager(table.Name, 5000, limits)
		if err != nil {
			t.Fatal(err)
		}
		page := ingest.Page{Total: int64(len(rows)), Records: rows}
		if err := pager.Accept(1, page); err != nil {
			t.Fatal(err)
		}
		request := ingest.PageRequest{Table: table.Name, Mode: ingest.Full, Page: 1, PageSize: 5000}
		if err := store.CommitPage(ctx, request, page, pager.Manifest().Pages[0]); err != nil {
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
	// A new configuration must not make a previously sealed large page invalid.
	files, options := targetPreparation(config.Runtime{SourceScope: "synthetic", SourcePageSize: 2, SpoolDirectory: t.TempDir(), SpoolBytes: 16 << 20}, quota, "Clargepage")
	options.Mode = ingest.Full
	manifest, err := files.Prepare(ctx, options, lease)
	if err != nil {
		t.Fatal("large sealed page could not be prepared", err)
	}
	for _, table := range manifest.Tables {
		if table.Table == "dwd_bzkscjzlb" {
			if table.Rows != 1001 {
				t.Fatal("large page was truncated", table.Rows)
			}
			return
		}
	}
	t.Fatal("grades missing from manifest")
}
