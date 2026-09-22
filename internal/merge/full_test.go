package merge_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

func sealedInput(t *testing.T, table string, mode ingest.Mode, rows []ingest.Record) (*staging.Sealed, string) {
	t.Helper()
	root := t.TempDir()
	files, err := staging.OpenDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	quota, err := staging.NewQuotaFiles(files, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	store, err := staging.NewStore("synthetic", quota, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	limits := ingest.PageLimits{MaxPages: 100, MaxRecords: 1000, MaxBytes: 1 << 20}
	for _, d := range ingest.Catalog() {
		var content []ingest.Record
		if d.Name == table {
			content = rows
		}
		request := ingest.PageRequest{Table: d.Name, Mode: mode, Page: 1, PageSize: 2}
		if mode == ingest.Incremental {
			request.StartTime = "2026-09-01 00:00:00"
			request.EndTime = "2026-09-01 01:00:00"
		}
		pager, err := ingest.NewPager(d.Name, 2, limits)
		if err != nil {
			t.Fatal(err)
		}
		for offset := 0; ; offset += 2 {
			end := offset + 2
			if end > len(content) {
				end = len(content)
			}
			page := ingest.Page{Total: int64(len(content)), Records: content[offset:end]}
			if err := pager.Accept(request.Page, page); err != nil {
				t.Fatal(err)
			}
			if err := store.CommitPage(context.Background(), request, page, pager.Manifest().Pages[request.Page-1]); err != nil {
				t.Fatal(err)
			}
			if end == len(content) {
				break
			}
			request.Page++
		}
	}
	ref, err := store.Seal(context.Background(), limits)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := store.OpenSealed(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return sealed, root
}

func baseline(t *testing.T, table string, rows []ingest.Record) merge.Baseline {
	t.Helper()
	base := merge.Baseline{Table: table, Scope: "mock-school/source-v1", Rows: rows}
	var err error
	base.SHA256, err = merge.SourceDigest(context.Background(), base.Table, base.Scope, rows)
	if err != nil {
		t.Fatal(err)
	}
	return base
}

func TestT04_12FullDeletesOnlyAfterCompleteSameScopeQualityGate(t *testing.T) {
	base := baseline(t, grades, records(t, grades, `{"ID":"01","XF":3}`, `{"ID":"02"}`, `{"ID":"03"}`))
	want := records(t, grades, `{"ID":"01","XF":3.0}`, `{"ID":"03"}`)
	for _, mode := range []ingest.Mode{ingest.Full, ingest.ForceFull} {
		sealed, _ := sealedInput(t, grades, mode, want)
		got, err := merge.Full(context.Background(), base, merge.FullInput{Scope: base.Scope, Sealed: sealed}, merge.Quality{MinimumRows: 2, MaximumDeletedRows: 1})
		if err != nil || !got.DeletionsKnown {
			t.Fatalf("valid full replacement failed: %v", err)
		}
		assertRows(t, grades, got.Rows, want)
		if len(got.Changes.Changes) != 2 || got.Changes.Changes[1].Kind != merge.Removed || got.Changes.Changes[1].Key != "02" {
			t.Fatalf("wrong deletion report: %+v", got.Changes)
		}
	}
	inc, err := merge.Incremental(context.Background(), grades, base.Rows, want)
	if err != nil || len(inc.Rows) != 3 || inc.DeletionsKnown {
		t.Fatal("incremental used full deletion branch")
	}
}

func TestT04_13FullRejectsUnverifiedBaselineRangeAndUnsafeDeletion(t *testing.T) {
	base := baseline(t, grades, records(t, grades, `{"ID":"01"}`, `{"ID":"02"}`))
	sealed, _ := sealedInput(t, grades, ingest.Full, records(t, grades, `{"ID":"01"}`))
	for _, test := range []struct {
		name   string
		base   merge.Baseline
		input  merge.FullInput
		policy merge.Quality
		want   error
	}{
		{"missing digest", merge.Baseline{Table: base.Table, Scope: base.Scope, Rows: base.Rows}, merge.FullInput{Scope: base.Scope, Sealed: sealed}, merge.Quality{MaximumDeletedRows: 2}, merge.ErrBaseline},
		{"changed baseline", merge.Baseline{Table: base.Table, Scope: base.Scope, SHA256: base.SHA256, Rows: records(t, grades, `{"ID":"01","XH":null}`, `{"ID":"02"}`)}, merge.FullInput{Scope: base.Scope, Sealed: sealed}, merge.Quality{MaximumDeletedRows: 2}, merge.ErrBaseline},
		{"range", base, merge.FullInput{Scope: "another-school", Sealed: sealed}, merge.Quality{MaximumDeletedRows: 2}, merge.ErrScope},
		{"minimum rows", base, merge.FullInput{Scope: base.Scope, Sealed: sealed}, merge.Quality{MinimumRows: 2, MaximumDeletedRows: 2}, merge.ErrQuality},
		{"deletion limit", base, merge.FullInput{Scope: base.Scope, Sealed: sealed}, merge.Quality{}, merge.ErrQuality},
		{"no sealed evidence", base, merge.FullInput{Scope: base.Scope}, merge.Quality{MaximumDeletedRows: 2}, merge.ErrInvalid},
		{"zero sealed handle", base, merge.FullInput{Scope: base.Scope, Sealed: &staging.Sealed{}}, merge.Quality{MaximumDeletedRows: 2}, merge.ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := merge.Full(context.Background(), test.base, test.input, test.policy)
			if !errors.Is(err, test.want) || got.Rows != nil {
				t.Fatalf("gate bypassed: %v", err)
			}
		})
	}
	empty, _ := sealedInput(t, grades, ingest.Full, nil)
	if got, err := merge.Full(context.Background(), base, merge.FullInput{Scope: base.Scope, Sealed: empty}, merge.Quality{MaximumDeletedRows: 2}); !errors.Is(err, merge.ErrQuality) || got.Rows != nil {
		t.Fatal("abnormal empty accepted")
	}
	got, err := merge.Full(context.Background(), baseline(t, grades, nil), merge.FullInput{Scope: base.Scope, Sealed: empty}, merge.Quality{})
	if err != nil || len(got.Rows) != 0 {
		t.Fatal("initial empty table rejected")
	}
	got, err = merge.Full(context.Background(), base, merge.FullInput{Scope: base.Scope, Sealed: empty}, merge.Quality{MaximumDeletedRows: 2, AllowEmpty: true})
	if err != nil || len(got.Rows) != 0 || !got.DeletionsKnown {
		t.Fatal("explicit empty exception failed")
	}
}

func TestT04_13FullRejectsIncrementalAndDamagedSealedPayload(t *testing.T) {
	rows := records(t, grades, `{"ID":"01"}`)
	base := baseline(t, grades, rows)
	sealed, _ := sealedInput(t, grades, ingest.Incremental, rows)
	if got, err := merge.Full(context.Background(), base, merge.FullInput{Scope: base.Scope, Sealed: sealed}, merge.Quality{}); !errors.Is(err, merge.ErrScope) || got.Rows != nil {
		t.Fatal("incremental evidence used as full")
	}
	sealed, root := sealedInput(t, grades, ingest.Full, rows)
	for _, entry := range sealed.Pages() {
		if entry.Request.Table == grades {
			if err := os.WriteFile(filepath.Join(root, entry.Payload), []byte("damaged"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got, err := merge.Full(context.Background(), base, merge.FullInput{Scope: base.Scope, Sealed: sealed}, merge.Quality{}); !errors.Is(err, staging.ErrCorrupt) || got.Rows != nil {
		t.Fatal("damaged source used")
	}
}

func TestT04_13FullHistoricalInputDeduplicatesAndChecksCurrentVersions(t *testing.T) {
	for _, d := range historyTables {
		old := historyRow(d.key, d.field, `"old"`, `"2026-09-01 01:00:00"`)
		latest := historyRow(d.key, d.field, `null`, `"2026-09-01 02:00:00"`)
		base := baseline(t, d.table, records(t, d.table, latest))
		sealed, _ := sealedInput(t, d.table, ingest.Full, records(t, d.table, old, old))
		got, err := merge.Full(context.Background(), base, merge.FullInput{Scope: base.Scope, Sealed: sealed}, merge.Quality{})
		if err != nil {
			t.Fatal(err)
		}
		assertRows(t, d.table, got.Rows, records(t, d.table, latest))
		conflict := historyRow(d.key, d.field, `"conflict"`, `"2026-09-01 02:00:00"`)
		newer := historyRow(d.key, d.field, `"newest"`, `"2026-09-01 03:00:00"`)
		sealed, _ = sealedInput(t, d.table, ingest.Full, records(t, d.table, conflict, newer))
		if got, err := merge.Full(context.Background(), base, merge.FullInput{Scope: base.Scope, Sealed: sealed}, merge.Quality{}); !errors.Is(err, merge.ErrHistoryConflict) || got.Rows != nil {
			t.Fatal("newest source hid baseline time conflict")
		}
	}
}

func TestT04_13OrdinaryDuplicateCannotBecomeFullAcquisition(t *testing.T) {
	pager, err := ingest.NewPager(grades, 2, ingest.PageLimits{MaxPages: 10, MaxRecords: 100, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	err = pager.Accept(1, ingest.Page{Total: 2, Records: records(t, grades, `{"ID":"01"}`, `{"ID":"01"}`)})
	if err == nil || pager.Manifest().Complete {
		t.Fatal("ordinary duplicate entered complete acquisition")
	}
}
