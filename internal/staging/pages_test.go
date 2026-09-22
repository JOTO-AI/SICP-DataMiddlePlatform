package staging_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

type memoryFiles struct {
	files                    map[string][]byte
	writes, publishes, syncs int
	failure                  string
}

func newMemoryFiles() *memoryFiles { return &memoryFiles{files: map[string][]byte{}} }
func (f *memoryFiles) Read(ctx context.Context, name string, limit int64) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	data, ok := f.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if int64(len(data)) > limit {
		return nil, staging.ErrCapacity
	}
	return append([]byte(nil), data...), nil
}
func (f *memoryFiles) WriteTemporary(ctx context.Context, data []byte) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	f.writes++
	name := fmt.Sprintf("pending-%d.tmp", f.writes)
	if (f.failure == "write_data" && f.writes == 1) || (f.failure == "write_receipt" && f.writes == 2) {
		f.files[name] = append([]byte(nil), data[:len(data)/2]...)
		f.failure = ""
		return name, errors.New("half write")
	}
	f.files[name] = append([]byte(nil), data...)
	return name, nil
}
func (f *memoryFiles) Publish(ctx context.Context, temp, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.publishes++
	receipt := strings.HasSuffix(name, ".receipt.json")
	if (f.failure == "publish_data" && !receipt) || (f.failure == "publish_receipt" && receipt) {
		f.failure = ""
		return errors.New("publication interruption")
	}
	if _, ok := f.files[name]; ok {
		return fs.ErrExist
	}
	f.files[name] = append([]byte(nil), f.files[temp]...)
	delete(f.files, temp)
	if (f.failure == "ack_data" && !receipt) || (f.failure == "ack_receipt" && receipt) {
		f.failure = ""
		return errors.New("publication acknowledgement lost")
	}
	return nil
}
func (f *memoryFiles) Sync(context.Context) error { f.syncs++; return nil }
func (f *memoryFiles) List(ctx context.Context, prefix string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var result []string
	for name := range f.files {
		if strings.HasPrefix(name, prefix) {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result, nil
}

func store(t *testing.T, files staging.Files) *staging.Store {
	t.Helper()
	s, err := staging.NewStore("run-001", files, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func stagedPage(t *testing.T) (ingest.PageRequest, ingest.Page, ingest.PageSummary) {
	t.Helper()
	request := ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: ingest.Full, Page: 1, PageSize: 2}
	page := ingest.Page{Total: 2}
	for _, raw := range []string{`{"XQH":"02"}`, `{"XQH":"01"}`} {
		r, err := ingest.DecodeRecord(request.Table, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		page.Records = append(page.Records, r)
	}
	pager, err := ingest.NewPager(request.Table, 2, ingest.PageLimits{MaxPages: 2, MaxRecords: 4, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err = pager.Accept(1, page); err != nil {
		t.Fatal(err)
	}
	return request, page, pager.Manifest().Pages[0]
}

func TestT04_01PartialWritesAndPublicationFailuresNeverConfirmHalfPage(t *testing.T) {
	for _, failure := range []string{"write_data", "publish_data", "write_receipt", "publish_receipt", "ack_data"} {
		t.Run(failure, func(t *testing.T) {
			files := newMemoryFiles()
			files.failure = failure
			s := store(t, files)
			r, p, m := stagedPage(t)
			if err := s.CommitPage(context.Background(), r, p, m); err == nil {
				t.Fatal("injected failure accepted")
			}
			entries, err := s.Entries(context.Background())
			if err != nil || len(entries) != 0 {
				t.Fatal("partial page entered manifest")
			}
			// Component reconstruction uses durable files, not an in-memory success bit.
			s = store(t, files)
			if err = s.CommitPage(context.Background(), r, p, m); err != nil {
				t.Fatal(err)
			}
			entries, err = s.Entries(context.Background())
			if err != nil || len(entries) != 1 || entries[0].Summary.Count != 2 {
				t.Fatal("retry did not recover exactly one page")
			}
		})
	}
}

func TestT04_02LostConfirmationAndRepeatedCommitAreIdempotent(t *testing.T) {
	files := newMemoryFiles()
	files.failure = "ack_receipt"
	s := store(t, files)
	r, p, m := stagedPage(t)
	if err := s.CommitPage(context.Background(), r, p, m); err == nil {
		t.Fatal("lost acknowledgement accepted")
	}
	entries, err := s.Entries(context.Background())
	if err != nil || len(entries) != 1 {
		t.Fatal("durable confirmation not recoverable")
	}
	writes := files.writes
	s = store(t, files)
	p.Records[0], p.Records[1] = p.Records[1], p.Records[0]
	for i := 0; i < 3; i++ {
		if err = s.CommitPage(context.Background(), r, p, m); err != nil {
			t.Fatal(err)
		}
	}
	if files.writes != writes || files.syncs == 0 {
		t.Fatal("confirmation rewrite or missing durable recheck")
	}
	got, err := s.Entries(context.Background())
	if err != nil || !reflect.DeepEqual(got, entries) {
		t.Fatal("confirmation retries changed manifest")
	}
	if string(files.files[got[0].Payload]) != "{\"XQH\":\"01\"}\n{\"XQH\":\"02\"}\n" {
		t.Fatal("payload content/order changed")
	}
}

func TestT04_02SamePageWithDifferentContentOrScopeConflicts(t *testing.T) {
	files := newMemoryFiles()
	s := store(t, files)
	r, p, m := stagedPage(t)
	if err := s.CommitPage(context.Background(), r, p, m); err != nil {
		t.Fatal(err)
	}
	writes := files.writes
	r.Mode = ingest.Incremental
	r.StartTime = "2026-08-05 07:55:00"
	r.EndTime = "2026-08-05 10:00:00"
	if err := s.CommitPage(context.Background(), r, p, m); !errors.Is(err, staging.ErrConflict) || files.writes != writes {
		t.Fatal("page scope silently replaced")
	}
	r.Mode = ingest.Full
	m.Digest[0]++
	if err := s.CommitPage(context.Background(), r, p, m); !errors.Is(err, staging.ErrInvalid) {
		t.Fatal("unverified evidence accepted")
	}
}

func TestT04_01CancelledAndInvalidInputDoNotWrite(t *testing.T) {
	files := newMemoryFiles()
	s := store(t, files)
	r, p, m := stagedPage(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.CommitPage(ctx, r, p, m); !errors.Is(err, context.Canceled) || files.writes != 0 {
		t.Fatal("cancelled page wrote data")
	}
	if _, err := staging.NewStore("../escape", files, 1024); err == nil {
		t.Fatal("unsafe run identity accepted")
	}
	r.Table = "../escape"
	if err := s.CommitPage(context.Background(), r, p, m); err == nil || files.writes != 0 {
		t.Fatal("unsafe source path accepted")
	}
}

func TestT04_01CorruptPayloadNeverCountsAsConfirmed(t *testing.T) {
	files := newMemoryFiles()
	s := store(t, files)
	r, p, m := stagedPage(t)
	if err := s.CommitPage(context.Background(), r, p, m); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files.files[entries[0].Payload] = []byte("partial")
	if _, err = s.Entries(context.Background()); !errors.Is(err, staging.ErrCorrupt) {
		t.Fatal("damaged page confirmed")
	}
}
