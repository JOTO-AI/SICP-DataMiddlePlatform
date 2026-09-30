package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

var (
	ErrInvalid  = errors.New("invalid staging input")
	ErrStorage  = errors.New("staging storage failure")
	ErrConflict = errors.New("staging page identity conflict")
	ErrCorrupt  = errors.New("staging content integrity failure")
	ErrCapacity = errors.New("staging capacity exceeded")
)
var runName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const receiptLimit = 16 << 10

type Entry struct {
	Version      int
	RunID        string
	Request      ingest.PageRequest
	Total        int64
	Summary      ingest.PageSummary
	Payload      string
	PayloadBytes int64
	SHA256       string
}
type Store struct {
	runID        string
	files        Files
	maxPageBytes int64
	mu           sync.Mutex
}

func NewStore(runID string, files *QuotaFiles, maxPageBytes int64) (*Store, error) {
	if !runName.MatchString(runID) || files == nil || files.files == nil || files.limit < 0 || maxPageBytes < 1 || maxPageBytes > 64<<20 {
		return nil, ErrInvalid
	}
	return &Store{runID: runID, files: files, maxPageBytes: maxPageBytes}, nil
}
func sourceKnown(name string) bool {
	for _, d := range ingest.Catalog() {
		if d.Name == name {
			return true
		}
	}
	return false
}
func normalized(r ingest.PageRequest) (ingest.PageRequest, error) {
	if !sourceKnown(r.Table) || r.Page < 1 || r.PageSize < 1 {
		return r, ErrInvalid
	}
	switch r.Mode {
	case ingest.Full, ingest.ForceFull:
		r.StartTime = ""
		r.EndTime = ""
	case ingest.Incremental:
		if r.StartTime == "" || r.EndTime == "" {
			return r, ErrInvalid
		}
	default:
		return r, ErrInvalid
	}
	return r, nil
}
func (s *Store) identity(r ingest.PageRequest) string {
	return fmt.Sprintf("%s.%s.%010d", s.runID, r.Table, r.Page)
}

func (s *Store) CommitPage(ctx context.Context, request ingest.PageRequest, page ingest.Page, summary ingest.PageSummary) error {
	q := s.files.(*QuotaFiles)
	q.lifecycle.Lock()
	defer q.lifecycle.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := s.notRetired(ctx); err != nil {
		return err
	}
	r, err := normalized(request)
	if err != nil {
		return err
	}
	if summary.Number != r.Page || summary.Count != len(page.Records) || summary.Count > r.PageSize || page.Total < int64(summary.Count) {
		return ErrInvalid
	}
	// Sort canonical rows for stable confirmation even if an equivalent retry
	// presents the same multiset in a different order. No source rows are dropped.
	rows := make([][]byte, 0, len(page.Records))
	hashes := make([][32]byte, 0, len(page.Records))
	var payloadSize, canonicalSize int64
	for _, record := range page.Records {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		raw, err := record.Canonical()
		if err != nil {
			return ErrInvalid
		}
		decoded, err := ingest.DecodeRecord(r.Table, raw)
		if err != nil || decoded.Key != record.Key {
			return ErrInvalid
		}
		if int64(len(raw))+1 > s.maxPageBytes-payloadSize {
			return ErrCapacity
		}
		payloadSize += int64(len(raw)) + 1
		canonicalSize += int64(len(raw))
		rows = append(rows, raw)
		hashes = append(hashes, sha256.Sum256(raw))
	}
	sort.Slice(hashes, func(i, j int) bool { return bytes.Compare(hashes[i][:], hashes[j][:]) < 0 })
	digest := sha256.New()
	for _, h := range hashes {
		digest.Write(h[:])
	}
	var actual [32]byte
	copy(actual[:], digest.Sum(nil))
	if canonicalSize != summary.Bytes || actual != summary.Digest {
		return ErrInvalid
	}
	sort.SliceStable(rows, func(i, j int) bool { return bytes.Compare(rows[i], rows[j]) < 0 })
	var payload bytes.Buffer
	for _, row := range rows {
		payload.Write(row)
		payload.WriteByte('\n')
	}
	hash := sha256.Sum256(payload.Bytes())
	hashText := hex.EncodeToString(hash[:])
	entry := Entry{Version: 1, RunID: s.runID, Request: r, Total: page.Total, Summary: summary, Payload: s.identity(r) + "." + hashText + ".jsonl", PayloadBytes: payloadSize, SHA256: hashText}
	receipt, err := json.Marshal(entry)
	if err != nil || len(receipt) > receiptLimit {
		return ErrInvalid
	}
	exists := false
	if existing, err := s.files.Read(ctx, s.identity(r)+".receipt.json", receiptLimit); err == nil {
		exists = true
		if !bytes.Equal(existing, receipt) {
			return ErrConflict
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return ErrStorage
	}
	if _, err := s.files.Read(ctx, s.runID+".sealed.json", sealLimit); err == nil {
		if !exists {
			return ErrSealed
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return ErrStorage
	}
	// Data publication alone does not confirm a page. Only the immutable
	// receipt is the authority used by Entries and later sealing.
	if err := s.ensure(ctx, entry.Payload, payload.Bytes(), s.maxPageBytes); err != nil {
		return err
	}
	return s.ensure(ctx, s.identity(r)+".receipt.json", receipt, receiptLimit)
}

func (s *Store) ensure(ctx context.Context, name string, data []byte, limit int64) error {
	current, err := s.files.Read(ctx, name, limit)
	if err == nil {
		if !bytes.Equal(current, data) {
			return ErrConflict
		}
		return s.files.Sync(ctx)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fileError(err)
	}
	temp, err := s.files.WriteTemporary(ctx, data)
	if err != nil {
		return fileError(err)
	}
	err = s.files.Publish(ctx, temp, name)
	if errors.Is(err, fs.ErrExist) {
		current, err = s.files.Read(ctx, name, limit)
		if err != nil {
			return ErrStorage
		}
		if !bytes.Equal(current, data) {
			return ErrConflict
		}
		return s.files.Sync(ctx)
	}
	if err != nil {
		return fileError(err)
	}
	return nil
}

func (s *Store) Entries(ctx context.Context) ([]Entry, error) {
	return s.entries(ctx, s.runID+".")
}
func (s *Store) entries(ctx context.Context, prefix string) ([]Entry, error) {
	names, err := s.files.List(ctx, prefix)
	if err != nil {
		return nil, ErrStorage
	}
	sort.Strings(names)
	entries := make([]Entry, 0, len(names))
	for _, name := range names {
		if !strings.HasSuffix(name, ".receipt.json") {
			continue
		}
		raw, err := s.files.Read(ctx, name, receiptLimit)
		if err != nil {
			return nil, ErrStorage
		}
		var e Entry
		if json.Unmarshal(raw, &e) != nil || e.Version != 1 || e.RunID != s.runID {
			return nil, ErrCorrupt
		}
		canonical, err := json.Marshal(e)
		if err != nil || !bytes.Equal(raw, canonical) {
			return nil, ErrCorrupt
		}
		r, err := normalized(e.Request)
		if err != nil || r != e.Request || name != s.identity(r)+".receipt.json" || e.Payload != s.identity(r)+"."+e.SHA256+".jsonl" || e.PayloadBytes < 0 || e.PayloadBytes > s.maxPageBytes || e.Summary.Number != r.Page || e.Summary.Count < 0 || e.Summary.Count > r.PageSize || e.Total < int64(e.Summary.Count) || e.Summary.Bytes < 0 || e.Summary.Bytes != e.PayloadBytes-int64(e.Summary.Count) {
			return nil, ErrCorrupt
		}
		hash, err := hex.DecodeString(e.SHA256)
		if err != nil || len(hash) != sha256.Size {
			return nil, ErrCorrupt
		}
		payload, err := s.files.Read(ctx, e.Payload, s.maxPageBytes)
		if err != nil {
			return nil, ErrCorrupt
		}
		actual := sha256.Sum256(payload)
		if int64(len(payload)) != e.PayloadBytes || !bytes.Equal(hash, actual[:]) {
			return nil, ErrCorrupt
		}
		entries = append(entries, e)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return entries, nil
}
