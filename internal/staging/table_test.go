package staging_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

func TestIncompleteTableCannotBeReusedOrLeavePartialCopiedInput(t *testing.T) {
	files := newMemoryFiles()
	q, _ := staging.NewQuotaFiles(files, 0)
	from, _ := staging.NewStore("from", q, 1<<20)
	to, _ := staging.NewStore("to", q, 1<<20)
	r, page, _ := stagedPage(t)
	page.Total = 3
	pager, _ := ingest.NewPager(r.Table, r.PageSize, sealLimits())
	if err := pager.Accept(1, page); err != nil {
		t.Fatal(err)
	}
	if err := from.CommitPage(t.Context(), r, page, pager.Manifest().Pages[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := from.CopyTableWithProgress(t.Context(), r.Table, to, ingest.Full, sealLimits(), func(p staging.TableCopyProgress) {
		if p.Phase == "reusing" {
			t.Fatal("partial table entered reuse phase")
		}
	}); !errors.Is(err, staging.ErrIncomplete) {
		t.Fatal(err)
	}
	entries, err := to.Entries(t.Context())
	if err != nil || len(entries) != 0 {
		t.Fatal("incomplete table copied")
	}
	// Failure/corruption in another table cannot invalidate this complete table.
	commitEmpty(t, from, "dwd_bzkscjzlb")
	files.files["from.dwd_xqjbsjzlb.0000000001.receipt.json"] = []byte("corrupt")
	if pages, err := from.CopyTable(t.Context(), "dwd_bzkscjzlb", to, ingest.Full, sealLimits()); err != nil || pages != 1 {
		t.Fatal("unrelated failed table prevented reuse", err)
	}
}

func TestTableReuseReportsVerifiedAndDurablyCopiedPagesSeparately(t *testing.T) {
	for _, failCopy := range []bool{false, true} {
		files := newMemoryFiles()
		q, _ := staging.NewQuotaFiles(files, 0)
		from, _ := staging.NewStore("from", q, 1<<20)
		to, _ := staging.NewStore("to", q, 1<<20)
		r, page, _ := stagedPage(t)
		r.PageSize = 1
		pager, _ := ingest.NewPager(r.Table, 1, sealLimits())
		for n, record := range page.Records {
			r.Page = n + 1
			part := ingest.Page{Total: 2, Records: []ingest.Record{record}}
			if err := pager.Accept(r.Page, part); err != nil {
				t.Fatal(err)
			}
			if err := from.CommitPage(t.Context(), r, part, pager.Manifest().Pages[n]); err != nil {
				t.Fatal(err)
			}
		}
		var events []staging.TableCopyProgress
		pages, err := from.CopyTableWithProgress(t.Context(), r.Table, to, ingest.Full, sealLimits(), func(p staging.TableCopyProgress) {
			events = append(events, p)
			copied, err := to.Entries(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if p.Phase == "checking" && len(copied) != 0 {
				t.Fatal("copy started before verification completed")
			}
			if p.Phase == "reusing" && int64(len(copied)) != p.CompletedPages {
				t.Fatal("progress precedes durable copy", p)
			}
			if failCopy && p.Phase == "reusing" && p.CompletedPages == 1 {
				files.failure = "publish_data"
			}
		})
		want := []staging.TableCopyProgress{{Phase: "checking"}, {Phase: "checking", TotalPages: 2}, {Phase: "checking", CompletedPages: 1, TotalPages: 2}, {Phase: "checking", CompletedPages: 2, TotalPages: 2}, {Phase: "reusing", TotalPages: 2}, {Phase: "reusing", CompletedPages: 1, TotalPages: 2}}
		if !failCopy {
			want = append(want, staging.TableCopyProgress{Phase: "reusing", CompletedPages: 2, TotalPages: 2})
		}
		if !reflect.DeepEqual(events, want) || (!failCopy && (err != nil || pages != 2)) || (failCopy && err == nil) {
			t.Fatal(events, pages, err)
		}
	}
}
