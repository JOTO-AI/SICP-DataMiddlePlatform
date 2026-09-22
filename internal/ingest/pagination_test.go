package ingest_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

func pager(t *testing.T, table string, size int) *ingest.Pager {
	t.Helper()
	p, err := ingest.NewPager(table, size, ingest.PageLimits{MaxPages: 10, MaxRecords: 100, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func sourcePage(t *testing.T, table string, total int64, rows ...string) ingest.Page {
	t.Helper()
	p := ingest.Page{Total: total, Records: []ingest.Record{}}
	for _, row := range rows {
		r, err := ingest.DecodeRecord(table, []byte(row))
		if err != nil {
			t.Fatal(err)
		}
		p.Records = append(p.Records, r)
	}
	return p
}
func expectPageFailure(t *testing.T, p *ingest.Pager, number int, page ingest.Page, kind ingest.FailureKind) {
	t.Helper()
	before := p.Manifest()
	err := p.Accept(number, page)
	var failure *ingest.Failure
	if !errors.As(err, &failure) || failure.Kind != kind || p.Manifest().Complete || p.Manifest().Rows != before.Rows || len(p.Manifest().Pages) != len(before.Pages) {
		t.Fatalf("bad pagination failure: %v", err)
	}
}

func TestT03_18ZeroTotalStillRequiresValidatedFirstPage(t *testing.T) {
	p := pager(t, "dwd_xqjbsjzlb", 2)
	if p.Manifest().Complete {
		t.Fatal("unfetched source complete")
	}
	if err := p.Accept(1, sourcePage(t, "dwd_xqjbsjzlb", 0)); err != nil {
		t.Fatal(err)
	}
	m := p.Manifest()
	if !m.Complete || len(m.Pages) != 1 || m.Rows != 0 {
		t.Fatal("empty source not complete")
	}
	m.Pages[0].Count = 9
	if p.Manifest().Pages[0].Count != 0 {
		t.Fatal("manifest aliases state")
	}
}

func TestT03_19ExpectedPageCountsAndNoExtraTail(t *testing.T) {
	for _, total := range []int64{4, 5} {
		p := pager(t, "dwd_xqjbsjzlb", 2)
		pages := [][]string{{`{"XQH":"01"}`, `{"XQH":"02"}`}, {`{"XQH":"03"}`, `{"XQH":"04"}`}}
		if total == 5 {
			pages = append(pages, []string{`{"XQH":"05"}`})
		}
		for i, rows := range pages {
			if err := p.Accept(i+1, sourcePage(t, "dwd_xqjbsjzlb", total, rows...)); err != nil {
				t.Fatal(err)
			}
			if p.Manifest().Complete != (i == len(pages)-1) {
				t.Fatal("completion boundary wrong")
			}
		}
		if p.Manifest().Rows != total || len(p.Manifest().Pages) != len(pages) {
			t.Fatal("wrong count")
		}
	}
}

func TestT03_20EmptyShortAndSkippedPagesInvalidateWholeSource(t *testing.T) {
	for _, number := range []int{2, 3} {
		p := pager(t, "dwd_xqjbsjzlb", 2)
		if err := p.Accept(1, sourcePage(t, "dwd_xqjbsjzlb", 5, `{"XQH":"01"}`, `{"XQH":"02"}`)); err != nil {
			t.Fatal(err)
		}
		expectPageFailure(t, p, number, sourcePage(t, "dwd_xqjbsjzlb", 5), ingest.PaginationFailure)
		if err := p.Accept(2, sourcePage(t, "dwd_xqjbsjzlb", 5, `{"XQH":"03"}`, `{"XQH":"04"}`)); err == nil {
			t.Fatal("failed source resumed")
		}
	}
	p := pager(t, "dwd_xqjbsjzlb", 2)
	expectPageFailure(t, p, 1, sourcePage(t, "dwd_xqjbsjzlb", 5, `{"XQH":"01"}`), ingest.PaginationFailure)
}

func TestT03_21OrdinaryDuplicateKeysAndHistoricalRepeatedPagesFail(t *testing.T) {
	for _, samePage := range []bool{false, true} {
		p := pager(t, "dwd_bzkskcsjzlb", 2)
		if samePage {
			expectPageFailure(t, p, 1, sourcePage(t, "dwd_bzkskcsjzlb", 2, `{"KCH":"01"}`, `{"KCH":"01"}`), ingest.DuplicateKey)
		} else {
			if err := p.Accept(1, sourcePage(t, "dwd_bzkskcsjzlb", 4, `{"KCH":"01"}`, `{"KCH":"02"}`)); err != nil {
				t.Fatal(err)
			}
			expectPageFailure(t, p, 2, sourcePage(t, "dwd_bzkskcsjzlb", 4, `{"KCH":"03"}`, `{"KCH":"01"}`), ingest.DuplicateKey)
		}
	}
	p := pager(t, "dwd_bzksgrtxsjzlb", 2)
	first := sourcePage(t, "dwd_bzksgrtxsjzlb", 4, `{"XH":"01","ETL_TIME":"2026-08-05 08:00:00"}`, `{"XH":"02","ETL_TIME":"2026-08-05 08:00:00"}`)
	if err := p.Accept(1, first); err != nil {
		t.Fatal(err)
	}
	second := sourcePage(t, "dwd_bzksgrtxsjzlb", 4, `{"ETL_TIME":"2026-08-05 08:00:00","XH":"02"}`, `{"ETL_TIME":"2026-08-05 08:00:00","XH":"01"}`)
	expectPageFailure(t, p, 2, second, ingest.DuplicatePage)
}

func TestT03_22ChangedTotalFails(t *testing.T) {
	p := pager(t, "dwd_xqjbsjzlb", 1)
	if err := p.Accept(1, sourcePage(t, "dwd_xqjbsjzlb", 2, `{"XQH":"01"}`)); err != nil {
		t.Fatal(err)
	}
	expectPageFailure(t, p, 2, sourcePage(t, "dwd_xqjbsjzlb", 3, `{"XQH":"02"}`), ingest.TotalChanged)
}

func TestT03_23PageRecordAndByteLimitsNeverTruncateSuccess(t *testing.T) {
	for _, limits := range []ingest.PageLimits{{MaxPages: 1, MaxRecords: 100, MaxBytes: 1000}, {MaxPages: 100, MaxRecords: 1, MaxBytes: 1000}, {MaxPages: 100, MaxRecords: 100, MaxBytes: 1}} {
		p, err := ingest.NewPager("dwd_xqjbsjzlb", 1, limits)
		if err != nil {
			t.Fatal(err)
		}
		expectPageFailure(t, p, 1, sourcePage(t, "dwd_xqjbsjzlb", 2, `{"XQH":"01"}`), ingest.AcquisitionLimit)
	}
}

func TestT03_27HistoricalRowsRetainVersionsAndCountSourceRows(t *testing.T) {
	p := pager(t, "dwd_bzksgrtxsjzlb", 1)
	for i, raw := range []string{`{"XH":"01","ETL_TIME":"2026-08-05 08:00:00"}`, `{"XH":"01","ETL_TIME":"2026-08-05 09:00:00"}`} {
		page := sourcePage(t, "dwd_bzksgrtxsjzlb", 2, raw)
		before := page.Records[0]
		if err := p.Accept(i+1, page); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, page.Records[0]) {
			t.Fatal("pager merged historical row")
		}
	}
	if !p.Manifest().Complete || p.Manifest().Rows != 2 {
		t.Fatal("source total compared with merged people count")
	}
}

func TestT03_21FingerprintPreservesTokensAndNestedDuplicateKeys(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"ID":"01","KCCJ":3.0}`, `{"ID":"01","KCCJ":3.0}`},
		{`{"ID":"01","KCCJ":"\ud800"}`, `{"ID":"01","KCCJ":"\ud800"}`},
		{`{"ID":"01","KCCJ":{"\ud800":"\udc00","normal":"\u0033"}}`, `{"ID":"01","KCCJ":{"normal":"3","\ud800":"\udc00"}}`},
		{`{"ID":"01","KCCJ":{"z":3.0,"a":[null," 3 "],"z":3}}`, `{"ID":"01","KCCJ":{"a":[null," 3 "],"z":3.0,"z":3}}`},
	} {
		r, err := ingest.DecodeRecord("dwd_bzkscjzlb", []byte(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		got, err := r.Canonical()
		if err != nil || string(got) != tc.want {
			t.Fatalf("canonical changed source semantics: %s", got)
		}
	}
}
