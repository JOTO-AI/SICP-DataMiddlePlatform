package merge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

var ErrLimit = errors.New("merge working resource limit exceeded")
var ErrSpool = errors.New("merge scratch storage failed integrity or IO validation")

type item struct {
	row   ingest.Record
	raw   []byte
	base  bool
	stamp time.Time
	timed bool
}

func makeItem(table string, r ingest.Record, base bool) (item, error) {
	copy, raw, err := validated(table, r)
	if err != nil {
		return item{}, err
	}
	stamp, timeErr := historyTime(copy)
	return item{row: copy, raw: raw, base: base, stamp: stamp, timed: timeErr == nil}, nil
}
func less(a, b item) bool {
	if a.row.Key != b.row.Key {
		return a.row.Key < b.row.Key
	}
	if a.timed != b.timed {
		return !a.timed
	}
	if !a.stamp.Equal(b.stamp) {
		return a.stamp.Before(b.stamp)
	}
	if order := bytes.Compare(a.raw, b.raw); order != 0 {
		return order < 0
	}
	return a.base && !b.base
}

type run struct {
	path string
	sum  [32]byte
	rows int64
}
type sorter struct {
	ctx         context.Context
	table, root string
	limits      Limits
	stats       *StreamStats
	buffer      []item
	bytes       int64
	runs        []run
	written     int64
	sequence    int
}

func (s *sorter) add(value item) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if int64(len(value.raw)) > s.limits.BatchBytes {
		return ErrLimit
	}
	if len(s.buffer) == s.limits.BatchRows || int64(len(value.raw)) > s.limits.BatchBytes-s.bytes {
		if err := s.flush(); err != nil {
			return err
		}
	}
	s.buffer = append(s.buffer, value)
	s.bytes += int64(len(value.raw))
	if len(s.buffer) > s.stats.PeakSortRows {
		s.stats.PeakSortRows = len(s.buffer)
	}
	if s.bytes > s.stats.PeakSortBytes {
		s.stats.PeakSortBytes = s.bytes
	}
	return nil
}
func (s *sorter) flush() error {
	if len(s.buffer) == 0 {
		return nil
	}
	if len(s.runs) >= s.limits.MaxRuns {
		return ErrLimit
	}
	sort.Slice(s.buffer, func(i, j int) bool { return less(s.buffer[i], s.buffer[j]) })
	w, err := s.writer()
	if err != nil {
		return err
	}
	defer w.file.Close()
	for _, entry := range s.buffer {
		if err := w.put(entry); err != nil {
			return err
		}
	}
	r, err := w.finish()
	if err != nil {
		return err
	}
	s.runs = append(s.runs, r)
	if len(s.runs) > s.stats.PeakRuns {
		s.stats.PeakRuns = len(s.runs)
	}
	s.buffer = nil
	s.bytes = 0
	return nil
}

type runWriter struct {
	sorter *sorter
	file   *os.File
	hash   hash.Hash
	rows   int64
}

func (s *sorter) writer() (*runWriter, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	s.sequence++
	file, err := os.OpenFile(filepath.Join(s.root, fmt.Sprintf("run-%09d.bin", s.sequence)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, ErrSpool
	}
	return &runWriter{sorter: s, file: file, hash: sha256.New()}, nil
}
func (w *runWriter) put(entry item) error {
	if err := w.sorter.ctx.Err(); err != nil {
		return err
	}
	charge := int64(len(entry.raw)) + 5
	if charge > w.sorter.limits.MaxSpoolBytes-w.sorter.written {
		return ErrLimit
	}
	var header [5]byte
	binary.BigEndian.PutUint32(header[:4], uint32(len(entry.raw)))
	if entry.base {
		header[4] = 1
	}
	writer := io.MultiWriter(w.file, w.hash)
	if _, err := writer.Write(header[:]); err != nil {
		return ErrSpool
	}
	if _, err := writer.Write(entry.raw); err != nil {
		return ErrSpool
	}
	w.sorter.written += charge
	w.rows++
	return nil
}
func (w *runWriter) finish() (run, error) {
	if err := w.file.Close(); err != nil {
		return run{}, ErrSpool
	}
	var sum [32]byte
	copy(sum[:], w.hash.Sum(nil))
	return run{path: w.file.Name(), sum: sum, rows: w.rows}, nil
}

type runReader struct {
	file           *os.File
	input          *bufio.Reader
	hash           hash.Hash
	expected       run
	rows           int64
	table          string
	done           bool
	maxRecordBytes int64
}

func openRun(table string, ref run, maxRecordBytes int64) (*runReader, error) {
	file, err := os.Open(ref.path)
	if err != nil {
		return nil, ErrSpool
	}
	h := sha256.New()
	return &runReader{file: file, input: bufio.NewReaderSize(io.TeeReader(file, h), 64<<10), hash: h, expected: ref, table: table, maxRecordBytes: maxRecordBytes}, nil
}
func (r *runReader) next() (item, error) {
	if r.done {
		return item{}, io.EOF
	}
	var header [5]byte
	_, err := io.ReadFull(r.input, header[:])
	if err == io.EOF {
		r.done = true
		if r.rows != r.expected.rows || !bytes.Equal(r.hash.Sum(nil), r.expected.sum[:]) {
			return item{}, ErrSpool
		}
		return item{}, io.EOF
	}
	if err != nil || header[4] > 1 {
		return item{}, ErrSpool
	}
	length := binary.BigEndian.Uint32(header[:4])
	if length == 0 || length > ingest.MaxRecordBytes || int64(length) > r.maxRecordBytes {
		return item{}, ErrSpool
	}
	raw := make([]byte, int(length))
	if _, err := io.ReadFull(r.input, raw); err != nil {
		return item{}, ErrSpool
	}
	row, err := ingest.DecodeRecord(r.table, raw)
	if err != nil {
		return item{}, ErrSpool
	}
	stamp, timeErr := historyTime(row)
	r.rows++
	return item{row: row, raw: raw, base: header[4] == 1, stamp: stamp, timed: timeErr == nil}, nil
}

// runMerge retains one row per input run; it never loads a complete run or key
// group. FanIn limits both open file descriptors and these lookahead records.
type runMerge struct {
	ctx     context.Context
	readers []*runReader
	heads   []*item
}

func openMerge(ctx context.Context, table string, runs []run, maxRecordBytes int64) (*runMerge, error) {
	m := &runMerge{ctx: ctx}
	for _, ref := range runs {
		r, err := openRun(table, ref, maxRecordBytes)
		if err != nil {
			m.close()
			return nil, err
		}
		m.readers = append(m.readers, r)
		entry, err := r.next()
		if err != nil && err != io.EOF {
			m.close()
			return nil, err
		}
		if err == io.EOF {
			m.heads = append(m.heads, nil)
		} else {
			m.heads = append(m.heads, &entry)
		}
	}
	return m, nil
}
func (m *runMerge) close() {
	for _, r := range m.readers {
		_ = r.file.Close()
	}
}
func (m *runMerge) next() (item, error) {
	if err := m.ctx.Err(); err != nil {
		return item{}, err
	}
	best := -1
	for i, head := range m.heads {
		if head != nil && (best < 0 || less(*head, *m.heads[best])) {
			best = i
		}
	}
	if best < 0 {
		return item{}, io.EOF
	}
	result := *m.heads[best]
	next, err := m.readers[best].next()
	if err == io.EOF {
		m.heads[best] = nil
	} else if err != nil {
		return item{}, err
	} else {
		m.heads[best] = &next
	}
	return result, nil
}
func (s *sorter) finish() (*runMerge, error) {
	if err := s.flush(); err != nil {
		return nil, err
	}
	for len(s.runs) > s.limits.FanIn {
		var next []run
		for start := 0; start < len(s.runs); start += s.limits.FanIn {
			end := start + s.limits.FanIn
			if end > len(s.runs) {
				end = len(s.runs)
			}
			group := s.runs[start:end]
			if len(group) > s.stats.PeakMergeReaders {
				s.stats.PeakMergeReaders = len(group)
			}
			m, err := openMerge(s.ctx, s.table, group, s.limits.BatchBytes)
			if err != nil {
				return nil, err
			}
			w, err := s.writer()
			if err != nil {
				m.close()
				return nil, err
			}
			for {
				entry, readErr := m.next()
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					m.close()
					w.file.Close()
					return nil, readErr
				}
				if err := w.put(entry); err != nil {
					m.close()
					w.file.Close()
					return nil, err
				}
			}
			m.close()
			ref, err := w.finish()
			if err != nil {
				return nil, err
			}
			next = append(next, ref)
			for _, old := range group {
				if err := os.Remove(old.path); err != nil {
					return nil, ErrSpool
				}
			}
		}
		s.runs = next
	}
	if len(s.runs) > s.stats.PeakMergeReaders {
		s.stats.PeakMergeReaders = len(s.runs)
	}
	s.stats.SpoolBytesWritten = s.written
	return openMerge(s.ctx, s.table, s.runs, s.limits.BatchBytes)
}
