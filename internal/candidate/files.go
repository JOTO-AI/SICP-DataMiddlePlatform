package candidate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const targetManifestLimit = 16 << 20

var targetName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// TargetFiles stores complete original-source targets, never derived business
// results. Control bundles/tasks retain their references through both builds,
// pagination retention, and recovery; P14 owns reference-aware collection.
type TargetFiles struct {
	Files         *staging.QuotaFiles
	MaxChunkBytes int64
	MaxChunks     int
}
type PrepareOptions struct {
	Combination, Scope, SpoolDirectory string
	Mode                               ingest.Mode
	Base                               *Manifest
	BaseSource                         CompleteSource
	Limits                             merge.Limits
	Quality                            map[string]merge.Quality
}
type targetChunk struct {
	Name, SHA256 string
	Bytes        int64
	Rows         int
}
type targetTable struct {
	Version TableVersion
	Chunks  []targetChunk
}
type targetDocument struct {
	Version  int
	Manifest Manifest
	Base     *Manifest
	Tables   []targetTable
	Changes  map[string]ChangeCounts `json:",omitempty"`
}

// ChangeCounts counts logical source-row changes once, independently of how
// many physical copies are subsequently written.
type ChangeCounts struct {
	Before, After, Added, Modified, Removed int64
	DeletionsKnown                          bool
}

func (f TargetFiles) valid() bool {
	return f.Files != nil && f.MaxChunkBytes > 0 && f.MaxChunkBytes <= 64<<20 && f.MaxChunks > 0 && f.MaxChunks <= 100000
}
func manifestName(combination string) string { return "target-" + combination + ".manifest.json" }
func chunkName(combination, table string, index int, sha string) string {
	return fmt.Sprintf("target-%s.%s.%08d.%s.jsonl", combination, table, index, sha)
}

func (f TargetFiles) put(ctx context.Context, name string, data []byte, limit int64) error {
	if int64(len(data)) > limit {
		return staging.ErrCapacity
	}
	if old, err := f.Files.Read(ctx, name, limit); err == nil {
		if !bytes.Equal(old, data) {
			return staging.ErrConflict
		}
		return f.Files.Sync(ctx)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	temp, err := f.Files.WriteTemporary(ctx, data)
	if err != nil {
		return err
	}
	if err = f.Files.Publish(ctx, temp, name); err == nil {
		return nil
	}
	// A lost durable-link acknowledgement is recoverable only for identical data
	// and an acknowledged directory sync; an arbitrary read is not write success.
	old, readErr := f.Files.Read(ctx, name, limit)
	if readErr != nil || !bytes.Equal(old, data) {
		return err
	}
	return f.Files.Sync(ctx)
}

type emptyRows struct{}

func (emptyRows) Next(context.Context) (ingest.Record, error) { return ingest.Record{}, io.EOF }

func (f TargetFiles) Prepare(ctx context.Context, o PrepareOptions, input *staging.Lease) (Manifest, error) {
	if !f.valid() || !targetName.MatchString(o.Combination) || o.Scope == "" || input == nil || o.Limits.BatchRows < 1 || o.Limits.BatchBytes < 1 || o.Limits.BatchBytes > f.MaxChunkBytes-int64(o.Limits.BatchRows) {
		return Manifest{}, ErrBuild
	}
	if o.Mode != ingest.Full && o.Mode != ingest.ForceFull && o.Mode != ingest.Incremental {
		return Manifest{}, ErrBuild
	}
	if o.Mode == ingest.Incremental && o.Base == nil {
		return Manifest{}, ErrBuild
	}
	activity.Report(ctx, "validating_input", "", "")
	if err := input.Validate(ctx); err != nil {
		return Manifest{}, err
	}
	for _, page := range input.Pages() {
		if page.Request.Mode != o.Mode {
			return Manifest{}, ErrBuild
		}
	}
	base := map[string]TableVersion{}
	if o.Base != nil {
		if o.Base.Validate() != nil || o.BaseSource == nil || o.Base.Combination == o.Combination {
			return Manifest{}, ErrBuild
		}
		if err := o.BaseSource.Validate(ctx, *o.Base); err != nil {
			return Manifest{}, err
		}
		for _, table := range o.Base.Tables {
			if table.Scope != o.Scope {
				return Manifest{}, ErrBuild
			}
			base[table.Table] = table
		}
	}
	doc := targetDocument{Version: 1, Manifest: Manifest{Combination: o.Combination, Input: input.Reference()}, Changes: map[string]ChangeCounts{}}
	if o.Base != nil {
		copy := *o.Base
		copy.Tables = append([]TableVersion(nil), o.Base.Tables...)
		doc.Base = &copy
	}
	totalChunks := 0
	for _, table := range source.Catalog() {
		activity.Report(ctx, "building_table", table.Name, "")
		var current merge.RowIterator = emptyRows{}
		baseDigest, err := merge.SourceDigest(ctx, table.Name, o.Scope, nil)
		if err != nil {
			return Manifest{}, err
		}
		var opened Rows
		if old, ok := base[table.Name]; ok {
			opened, err = o.BaseSource.Open(ctx, *o.Base, old)
			if err != nil {
				return Manifest{}, err
			}
			if opened == nil {
				return Manifest{}, ErrBuild
			}
			current = opened
			baseDigest = old.SHA256
		}
		sink := &fileSink{files: f, combination: o.Combination, table: table.Name, remaining: f.MaxChunks - totalChunks}
		summary, err := merge.Stream(ctx, merge.StreamOptions{Table: table.Name, Scope: o.Scope, BaselineSHA256: baseDigest, SpoolDirectory: o.SpoolDirectory, Limits: o.Limits, Quality: o.Quality[table.Name]}, current, merge.SealedInput{Scope: o.Scope, Lease: input}, sink)
		if opened != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			err = errors.Join(err, opened.Close(cleanup))
			cancel()
		}
		if err != nil {
			return Manifest{}, err
		}
		if !sink.complete || summary.Input != doc.Manifest.Input {
			return Manifest{}, ErrBuild
		}
		version := TableVersion{Table: table.Name, Scope: o.Scope, SHA256: summary.SHA256, Rows: summary.AfterRows}
		doc.Changes[table.Name] = ChangeCounts{Before: summary.BeforeRows, After: summary.AfterRows, Added: summary.Added, Modified: summary.Modified, Removed: summary.Removed, DeletionsKnown: summary.DeletionsKnown}
		doc.Manifest.Tables = append(doc.Manifest.Tables, version)
		doc.Tables = append(doc.Tables, targetTable{Version: version, Chunks: sink.chunks})
		totalChunks += len(sink.chunks)
	}
	if err := doc.Manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	activity.Report(ctx, "validating_target", "", "")
	if err := input.Validate(ctx); err != nil {
		return Manifest{}, err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return Manifest{}, err
	}
	if err = f.put(ctx, manifestName(o.Combination), raw, targetManifestLimit); err != nil {
		return Manifest{}, err
	}
	if err = f.Validate(ctx, doc.Manifest); err != nil {
		return Manifest{}, err
	}
	return doc.Manifest, nil
}

type fileSink struct {
	files              TargetFiles
	combination, table string
	chunks             []targetChunk
	remaining          int
	complete           bool
}

func (s *fileSink) WriteBatch(ctx context.Context, rows []ingest.Record) error {
	if len(rows) == 0 || len(s.chunks) >= s.remaining {
		return staging.ErrCapacity
	}
	var buf bytes.Buffer
	for _, row := range rows {
		raw, err := row.Canonical()
		if err != nil {
			return err
		}
		if int64(len(raw)+1) > s.files.MaxChunkBytes-int64(buf.Len()) {
			return staging.ErrCapacity
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	raw := buf.Bytes()
	hash := sha256.Sum256(raw)
	sha := hex.EncodeToString(hash[:])
	name := chunkName(s.combination, s.table, len(s.chunks), sha)
	if err := s.files.put(ctx, name, raw, s.files.MaxChunkBytes); err != nil {
		return err
	}
	s.chunks = append(s.chunks, targetChunk{Name: name, SHA256: sha, Bytes: int64(len(raw)), Rows: len(rows)})
	return nil
}
func (s *fileSink) Complete(context.Context, merge.Summary) error { s.complete = true; return nil }
func (s *fileSink) Abort(context.Context) error                   { s.complete = false; return nil } // Unreferenced immutable chunks remain quarantined for GC.

func (f TargetFiles) document(ctx context.Context, m Manifest) (targetDocument, error) {
	if !f.valid() || !targetName.MatchString(m.Combination) || m.Validate() != nil {
		return targetDocument{}, ErrBuild
	}
	raw, err := f.Files.Read(ctx, manifestName(m.Combination), targetManifestLimit)
	if err != nil {
		return targetDocument{}, err
	}
	var doc targetDocument
	if json.Unmarshal(raw, &doc) != nil || doc.Version != 1 || len(doc.Tables) != 14 {
		return targetDocument{}, ErrBuild
	}
	canonical, err := json.Marshal(doc)
	if err != nil || !bytes.Equal(canonical, raw) {
		return targetDocument{}, ErrBuild
	}
	want, _ := json.Marshal(m)
	actual, _ := json.Marshal(doc.Manifest)
	if !bytes.Equal(want, actual) {
		return targetDocument{}, ErrBuild
	}
	if doc.Base != nil {
		if doc.Base.Validate() != nil || doc.Base.Combination == m.Combination {
			return targetDocument{}, ErrBuild
		}
		for _, old := range doc.Base.Tables {
			for _, current := range m.Tables {
				if old.Table == current.Table && old.Scope != current.Scope {
					return targetDocument{}, ErrBuild
				}
			}
		}
	}
	chunks := 0
	for i, table := range doc.Tables {
		if table.Version != m.Tables[i] {
			return targetDocument{}, ErrBuild
		}
		var rows int64
		for index, c := range table.Chunks {
			chunks++
			if chunks > f.MaxChunks || c.Bytes < 1 || c.Bytes > f.MaxChunkBytes || c.Rows < 1 || c.Rows > 100000 || !digest(c.SHA256) || c.Name != chunkName(m.Combination, table.Version.Table, index, c.SHA256) {
				return targetDocument{}, ErrBuild
			}
			rows += int64(c.Rows)
		}
		if rows != table.Version.Rows {
			return targetDocument{}, ErrBuild
		}
	}
	return doc, nil
}
func (f TargetFiles) Validate(ctx context.Context, m Manifest) error {
	doc, err := f.document(ctx, m)
	if err != nil {
		return err
	}
	for _, table := range doc.Tables {
		rows := &fileRows{files: f, table: table.Version.Table, chunks: table.Chunks}
		checked := newChecked(table.Version, rows)
		for {
			_, more, err := checked.Next(ctx)
			if err != nil {
				return err
			}
			if !more {
				break
			}
		}
	}
	return ctx.Err()
}
func (f TargetFiles) Changes(ctx context.Context, m Manifest) (map[string]ChangeCounts, error) {
	doc, err := f.document(ctx, m)
	if err != nil {
		return nil, err
	}
	if len(doc.Changes) != len(m.Tables) {
		return nil, ErrBuild
	}
	for _, v := range m.Tables {
		c, ok := doc.Changes[v.Table]
		if !ok || c.Before < 0 || c.After != v.Rows || c.Added < 0 || c.Modified < 0 || c.Removed < 0 || c.Removed > c.Before || c.Added > c.After || c.Modified > c.After-c.Added || c.Before-c.Removed != c.After-c.Added || (!c.DeletionsKnown && c.Removed != 0) {
			return nil, ErrBuild
		}
	}
	return doc.Changes, nil
}

func (f TargetFiles) Open(ctx context.Context, m Manifest, v TableVersion) (Rows, error) {
	doc, err := f.document(ctx, m)
	if err != nil {
		return nil, err
	}
	for _, table := range doc.Tables {
		if table.Version == v {
			return &fileRows{files: f, table: v.Table, chunks: table.Chunks}, nil
		}
	}
	return nil, ErrBuild
}

type fileRows struct {
	files       TargetFiles
	table       string
	chunks      []targetChunk
	chunk, line int
	lines       [][]byte
	closed      bool
}

func (r *fileRows) Next(ctx context.Context) (ingest.Record, error) {
	if err := ctx.Err(); err != nil {
		return ingest.Record{}, err
	}
	if r.closed {
		return ingest.Record{}, ErrBuild
	}
	if r.line == len(r.lines) {
		if r.chunk == len(r.chunks) {
			return ingest.Record{}, io.EOF
		}
		chunk := r.chunks[r.chunk]
		raw, err := r.files.Files.Read(ctx, chunk.Name, r.files.MaxChunkBytes)
		if err != nil {
			return ingest.Record{}, err
		}
		hash := sha256.Sum256(raw)
		if int64(len(raw)) != chunk.Bytes || hex.EncodeToString(hash[:]) != chunk.SHA256 || len(raw) == 0 || raw[len(raw)-1] != '\n' {
			return ingest.Record{}, ErrBuild
		}
		r.lines = bytes.Split(raw[:len(raw)-1], []byte{'\n'})
		if len(r.lines) != chunk.Rows {
			return ingest.Record{}, ErrBuild
		}
		r.chunk++
		r.line = 0
	}
	raw := r.lines[r.line]
	r.line++
	return ingest.DecodeRecord(r.table, raw)
}
func (r *fileRows) Close(context.Context) error { r.closed = true; r.lines = nil; return nil }
