package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

var ErrIncomplete = errors.New("staging acquisition is incomplete")
var ErrSealed = errors.New("staging acquisition is sealed")

const sealLimit = 16 << 20

type SealRef struct{ RunID, SHA256 string }
type sealDocument struct {
	Version int
	RunID   string
	Limits  ingest.PageLimits
	Pages   []Entry
	Tables  []ingest.Manifest
}

// Seal reconstructs completeness from durable receipts and payloads. Caller
// success flags cannot substitute for a complete set of all fourteen sources.
func (s *Store) Seal(ctx context.Context, limits ingest.PageLimits) (SealRef, error) {
	q := s.files.(*QuotaFiles)
	q.lifecycle.Lock()
	defer q.lifecycle.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.notRetired(ctx); err != nil {
		return SealRef{}, err
	}
	entries, err := s.Entries(ctx)
	if err != nil {
		return SealRef{}, err
	}
	tables, err := s.verifyEntries(ctx, entries, limits)
	if err != nil {
		return SealRef{}, err
	}
	doc := sealDocument{Version: 1, RunID: s.runID, Limits: limits, Pages: entries, Tables: tables}
	raw, err := json.Marshal(doc)
	if err != nil {
		return SealRef{}, ErrInvalid
	}
	if len(raw) > sealLimit {
		return SealRef{}, ErrCapacity
	}
	if err = s.ensure(ctx, s.runID+".sealed.json", raw, sealLimit); err != nil {
		return SealRef{}, err
	}
	hash := sha256.Sum256(raw)
	return SealRef{RunID: s.runID, SHA256: hex.EncodeToString(hash[:])}, nil
}

// Sealed exposes verified reads, not unchecked source file paths. Each page
// read rechecks its payload digest to catch damage after initial verification.
type Sealed struct {
	store     *Store
	reference SealRef
	entries   []Entry
	tables    []ingest.Manifest
}

func (s *Sealed) Reference() SealRef { return s.reference }
func (s *Sealed) Pages() []Entry     { return append([]Entry(nil), s.entries...) }
func (s *Sealed) ReadPage(ctx context.Context, index int) (ingest.Page, error) {
	if index < 0 || index >= len(s.entries) {
		return ingest.Page{}, ErrInvalid
	}
	return s.store.readPage(ctx, s.entries[index])
}

func (s *Store) OpenSealed(ctx context.Context, reference SealRef) (*Sealed, error) {
	if err := s.notRetired(ctx); err != nil {
		return nil, err
	}
	if reference.RunID != s.runID {
		return nil, ErrInvalid
	}
	raw, err := s.files.Read(ctx, s.runID+".sealed.json", sealLimit)
	if err != nil {
		return nil, ErrCorrupt
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != reference.SHA256 {
		return nil, ErrCorrupt
	}
	var doc sealDocument
	if json.Unmarshal(raw, &doc) != nil || doc.Version != 1 || doc.RunID != s.runID {
		return nil, ErrCorrupt
	}
	canonical, err := json.Marshal(doc)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, ErrCorrupt
	}
	entries, err := s.Entries(ctx)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(entries, doc.Pages) {
		return nil, ErrCorrupt
	}
	tables, err := s.verifyEntries(ctx, entries, doc.Limits)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(tables, doc.Tables) {
		return nil, ErrCorrupt
	}
	return &Sealed{store: s, reference: reference, entries: entries, tables: tables}, nil
}

func (s *Store) verifyEntries(ctx context.Context, entries []Entry, limits ingest.PageLimits) (result []ingest.Manifest, err error) {
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "input_check"}, "")
	defer func() { end(err) }()
	bySource := map[string][]Entry{}
	for _, entry := range entries {
		if entry.Request.Mode != entries[0].Request.Mode || (entry.Request.Mode == ingest.Incremental && entry.Request.EndTime != entries[0].Request.EndTime) {
			return nil, ErrConflict
		}
		bySource[entry.Request.Table] = append(bySource[entry.Request.Table], entry)
	}
	if len(bySource) != len(ingest.Catalog()) {
		return nil, ErrIncomplete
	}
	var manifests []ingest.Manifest
	for _, definition := range ingest.Catalog() {
		pages := bySource[definition.Name]
		if len(pages) == 0 {
			return nil, ErrIncomplete
		}
		scope := pages[0].Request
		scope.Page = 1
		pager, err := ingest.NewPager(definition.Name, scope.PageSize, limits)
		if err != nil {
			return nil, ErrInvalid
		}
		for _, entry := range pages {
			r := entry.Request
			r.Page = 1
			if r != scope {
				return nil, ErrConflict
			}
			page, err := s.readPage(ctx, entry)
			if err != nil {
				return nil, err
			}
			if err = pager.Accept(entry.Request.Page, page); err != nil {
				return nil, ErrIncomplete
			}
			current := pager.Manifest()
			if current.Pages[len(current.Pages)-1] != entry.Summary {
				return nil, ErrCorrupt
			}
		}
		manifest := pager.Manifest()
		if !manifest.Complete {
			return nil, ErrIncomplete
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

func (s *Store) readPage(ctx context.Context, entry Entry) (ingest.Page, error) {
	raw, err := s.files.Read(ctx, entry.Payload, s.maxPageBytes)
	if err != nil {
		return ingest.Page{}, ErrCorrupt
	}
	hash := sha256.Sum256(raw)
	buildmetric.Add(ctx, buildmetric.FileValidations, 1)
	buildmetric.Add(ctx, buildmetric.BytesRead, int64(len(raw)))
	buffered := int64(len(raw))
	buildmetric.Memory(ctx, buffered)
	defer buildmetric.Memory(ctx, -buffered)
	if hex.EncodeToString(hash[:]) != entry.SHA256 || int64(len(raw)) != entry.PayloadBytes {
		return ingest.Page{}, ErrCorrupt
	}
	page := ingest.Page{Total: entry.Total, Records: []ingest.Record{}}
	for len(raw) > 0 {
		if ctx.Err() != nil {
			return ingest.Page{}, ctx.Err()
		}
		end := bytes.IndexByte(raw, '\n')
		if end < 0 || end == 0 {
			return ingest.Page{}, ErrCorrupt
		}
		record, err := ingest.DecodeRecord(entry.Request.Table, raw[:end])
		buildmetric.Add(ctx, buildmetric.Decodes, 1)
		buildmetric.Add(ctx, buildmetric.RowsRead, 1)
		if err != nil {
			return ingest.Page{}, ErrCorrupt
		}
		page.Records = append(page.Records, record)
		raw = raw[end+1:]
	}
	if len(page.Records) != entry.Summary.Count {
		return ingest.Page{}, ErrCorrupt
	}
	return page, nil
}
