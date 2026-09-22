package staging_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func sealLimits() ingest.PageLimits {
	return ingest.PageLimits{MaxPages: 10, MaxRecords: 100, MaxBytes: 1 << 20}
}
func commitEmpty(t *testing.T, s *staging.Store, table string) (ingest.PageRequest, ingest.Page, ingest.PageSummary) {
	t.Helper()
	r := ingest.PageRequest{Table: table, Mode: ingest.Full, Page: 1, PageSize: 2}
	page := ingest.Page{Total: 0}
	pager, err := ingest.NewPager(table, 2, sealLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err = pager.Accept(1, page); err != nil {
		t.Fatal(err)
	}
	summary := pager.Manifest().Pages[0]
	if err = s.CommitPage(context.Background(), r, page, summary); err != nil {
		t.Fatal(err)
	}
	return r, page, summary
}
func completeStore(t *testing.T) (*staging.Store, *memoryFiles, staging.SealRef) {
	t.Helper()
	files := newMemoryFiles()
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
	return s, files, ref
}

func TestT04_03ThirteenSourcesCannotSealButFourteenCan(t *testing.T) {
	files := newMemoryFiles()
	s := store(t, files)
	catalog := ingest.Catalog()
	for _, d := range catalog[:13] {
		commitEmpty(t, s, d.Name)
	}
	if _, err := s.Seal(context.Background(), sealLimits()); !errors.Is(err, staging.ErrIncomplete) {
		t.Fatal("thirteen sources sealed")
	}
	if _, ok := files.files["run-001.sealed.json"]; ok {
		t.Fatal("failed seal published file")
	}
	r, p, m := commitEmpty(t, s, catalog[13].Name)
	ref, err := s.Seal(context.Background(), sealLimits())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.OpenSealed(context.Background(), ref)
	if err != nil || len(sealed.Pages()) != 14 {
		t.Fatalf("complete source seal failed: %v", err)
	}
	writes := files.writes
	again, err := s.Seal(context.Background(), sealLimits())
	if err != nil || again != ref || files.writes != writes {
		t.Fatal("seal retry changed evidence")
	}
	if err = s.CommitPage(context.Background(), r, p, m); err != nil {
		t.Fatal("identical post-seal confirmation rejected")
	}
	r.Page = 2
	m.Number = 2
	if err = s.CommitPage(context.Background(), r, p, m); !errors.Is(err, staging.ErrSealed) || files.writes != writes {
		t.Fatal("sealed source accepted new page")
	}
}

func TestT04_04SealAndPayloadChangesFailBeforeUse(t *testing.T) {
	for _, target := range []string{"seal", "payload", "receipt"} {
		t.Run(target, func(t *testing.T) {
			s, files, ref := completeStore(t)
			sealed, err := s.OpenSealed(context.Background(), ref)
			if err != nil {
				t.Fatal(err)
			}
			switch target {
			case "seal":
				files.files["run-001.sealed.json"] = append(files.files["run-001.sealed.json"], ' ')
			case "payload":
				files.files[sealed.Pages()[0].Payload] = []byte("changed")
			case "receipt":
				for name := range files.files {
					if strings.HasSuffix(name, ".receipt.json") {
						files.files[name] = []byte("{}")
						break
					}
				}
			}
			if _, err = s.OpenSealed(context.Background(), ref); err == nil {
				t.Fatal("damaged source became importable")
			}
			if target == "payload" {
				if _, err = sealed.ReadPage(context.Background(), 0); !errors.Is(err, staging.ErrCorrupt) {
					t.Fatal("read after validation ignored later damage")
				}
			}
		})
	}
}

func TestT04_03SealingRechecksPageContinuityAndScope(t *testing.T) {
	for _, mixedScope := range []bool{false, true} {
		files := newMemoryFiles()
		s := store(t, files)
		for _, d := range ingest.Catalog() {
			if d.Name != "dwd_xqjbsjzlb" {
				commitEmpty(t, s, d.Name)
			}
		}
		request := ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: ingest.Full, Page: 1, PageSize: 1}
		pager, _ := ingest.NewPager(request.Table, 1, sealLimits())
		for i, raw := range []string{`{"XQH":"01"}`, `{"XQH":"02"}`} {
			record, err := ingest.DecodeRecord(request.Table, []byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			page := ingest.Page{Total: 2, Records: []ingest.Record{record}}
			if err = pager.Accept(i+1, page); err != nil {
				t.Fatal(err)
			}
			summary := pager.Manifest().Pages[i]
			request.Page = i + 1
			if i == 1 {
				if !mixedScope {
					continue
				}
				request.Mode = ingest.Incremental
				request.StartTime = "2026-08-05 07:55:00"
				request.EndTime = "2026-08-05 10:00:00"
			}
			if err = s.CommitPage(context.Background(), request, page, summary); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Seal(context.Background(), sealLimits()); err == nil {
			t.Fatal("incomplete or mixed source sealed")
		}
	}
}

func TestT04_05QuotaIncludesOrphansAndDoesNotRetryLocalFailure(t *testing.T) {
	files := newMemoryFiles()
	files.files["pending-orphan.tmp"] = []byte(strings.Repeat("x", 100))
	quota, err := staging.NewQuotaFiles(files, 101)
	if err != nil {
		t.Fatal(err)
	}
	s := store(t, quota)
	r, p, m := stagedPage(t)
	if err = s.CommitPage(context.Background(), r, p, m); !errors.Is(err, staging.ErrCapacity) || files.writes != 0 {
		t.Fatal("orphan bytes bypassed capacity")
	}
	entries, err := s.Entries(context.Background())
	if err != nil || len(entries) != 0 {
		t.Fatal("capacity failure confirmed page")
	}
	files = newMemoryFiles()
	quota, err = staging.NewQuotaFiles(files, 30)
	if err != nil {
		t.Fatal(err)
	}
	s = store(t, quota)
	// Payload fits, receipt does not. A published data file is still not a
	// successfully confirmed source page and remains charged to the quota.
	if err = s.CommitPage(context.Background(), r, p, m); !errors.Is(err, staging.ErrCapacity) {
		t.Fatal("receipt escaped quota")
	}
	entries, err = s.Entries(context.Background())
	if err != nil || len(entries) != 0 {
		t.Fatal("unconfirmed data counted")
	}
}

type stagingTransport struct{ calls int }

func (t *stagingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":200,"message":"ok","data":{"total":3,"list":[{"XQH":"01"}]}}`)), Header: make(http.Header)}, nil
}

type stagingTimer struct{ time.Time }

func (c stagingTimer) Now() time.Time { return c.Time }
func (stagingTimer) Wait(context.Context, time.Duration) error {
	return errors.New("unexpected retry wait")
}

type noCooldown struct{}

func (noCooldown) Cooldown(context.Context) (time.Time, error)     { return time.Time{}, nil }
func (noCooldown) ExtendCooldown(context.Context, time.Time) error { return nil }

func TestT04_05CollectorStopsSourceCallsWhenStagingIsFull(t *testing.T) {
	transport := &stagingTransport{}
	client, err := ingest.NewClient("https://mock.invalid", "fictional", transport, 1024)
	if err != nil {
		t.Fatal(err)
	}
	clock := stagingTimer{time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}
	limiter, err := ingest.NewLimiter(1)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ingest.NewCoordinator(client, ingest.BudgetConfig{RequestTimeout: time.Second, PageTimeout: time.Minute, InitialBackoff: time.Second, MaxBackoff: time.Second, MaxAttempts: 3}, clock, testkit.FixedRandom(0), noCooldown{}, limiter)
	if err != nil {
		t.Fatal(err)
	}
	files := newMemoryFiles()
	quota, err := staging.NewQuotaFiles(files, 1)
	if err != nil {
		t.Fatal(err)
	}
	s := store(t, quota)
	task, err := ingest.NewTaskBudget(clock.Now().Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Collect(context.Background(), ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: ingest.Full, Page: 1, PageSize: 1}, task, sealLimits(), s)
	if err == nil || transport.calls != 1 || files.writes != 0 || r.Manifest.Complete || r.Manifest.Rows != 0 {
		t.Fatal("local capacity failure retried upstream or completed")
	}
}

func TestT04_05SharedQuotaSerializesConcurrentReservations(t *testing.T) {
	files := newMemoryFiles()
	quota, err := staging.NewQuotaFiles(files, 100)
	if err != nil {
		t.Fatal(err)
	}
	var success atomic.Int32
	var unexpected atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := quota.WriteTemporary(context.Background(), []byte("0123456789")); err == nil {
				success.Add(1)
			} else if !errors.Is(err, staging.ErrCapacity) {
				unexpected.Add(1)
			}
		}()
	}
	wg.Wait()
	used, err := quota.Usage(context.Background())
	if err != nil || used != 100 || success.Load() != 10 || unexpected.Load() != 0 {
		t.Fatal("concurrent writes exceeded quota")
	}
}
