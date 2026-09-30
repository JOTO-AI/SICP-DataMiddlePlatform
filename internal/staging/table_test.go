package staging_test

import (
	"errors"
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
	if _, err := from.CopyTable(t.Context(), r.Table, to, ingest.Full, sealLimits()); !errors.Is(err, staging.ErrIncomplete) {
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
