package merge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

// Limits bounds data buffers, merge descriptors, run metadata and scratch IO.
// BatchBytes counts canonical token bytes, not Go allocator overhead. Rows
// additionally bound the per-record maps; input page allocation is independent.
// MaxSpoolBytes bounds cumulative scratch writes (and therefore disk occupancy).
type Limits struct {
	BatchRows, FanIn, MaxRuns int
	BatchBytes, MaxSpoolBytes int64
	InputPageRows             int
	InputPageBytes            int64
}
type StreamStats struct {
	PeakSortRows, PeakRuns, PeakMergeReaders, PeakOutputRows, PeakInputPageRows int
	PeakSortBytes, PeakOutputBytes, PeakInputPageBytes, SpoolBytesWritten       int64
}
type Summary struct {
	BeforeRows, AfterRows, Added, Modified, Removed int64
	DeletionsKnown                                  bool
	SHA256                                          string
	Input                                           staging.SealRef
	Buffers                                         StreamStats
}
type StreamOptions struct {
	Table, Scope, BaselineSHA256, SpoolDirectory string
	Limits                                       Limits
	Quality                                      Quality
}
type SealedInput struct {
	Scope string // From the persisted trusted task/source configuration.
	Lease *staging.Lease
}

// RowIterator returns original current-table source evidence, ending with EOF.
// Its allocation must be bounded by its adapter. Stream verifies its digest and
// primary key uniqueness before completing any candidate.
type RowIterator interface {
	Next(context.Context) (ingest.Record, error)
}

// CandidateSink consumes each batch synchronously without retaining its backing
// storage. Writes remain unpublished. Complete means merge validation succeeded,
// never publication approval. Abort must leave partial targets non-ready.
type CandidateSink interface {
	WriteBatch(context.Context, []ingest.Record) error
	Complete(context.Context, Summary) error
	Abort(context.Context) error
}

func digestHeader(table, scope string) hash.Hash {
	h := sha256.New()
	header, _ := json.Marshal(struct{ Table, Scope string }{table, scope})
	h.Write(header)
	h.Write([]byte{'\n'})
	return h
}
func writeDigest(h hash.Hash, raw []byte) { h.Write(raw); h.Write([]byte{'\n'}) }

func validLimits(l Limits) bool {
	return l.BatchRows > 0 && l.BatchRows <= 100000 && l.BatchBytes > 0 && l.BatchBytes <= 64<<20 && l.FanIn >= 2 && l.FanIn <= 32 && l.MaxRuns > 0 && l.MaxRuns <= 100000 && l.MaxSpoolBytes > 0 && l.InputPageRows > 0 && l.InputPageRows <= 100000 && l.InputPageBytes > 0 && l.InputPageBytes <= 64<<20
}

// Stream uses only an actual active sealed-acquisition lease for incoming rows.
// Arbitrary iterators cannot claim to be a complete full source. It externally
// sorts bounded runs, merges with bounded fan-in and folds each key in constant
// state, even when one person has arbitrarily many historical source versions.
func Stream(ctx context.Context, options StreamOptions, base RowIterator, input SealedInput, sink CandidateSink) (summary Summary, err error) {
	if sink == nil {
		return Summary{}, ErrInvalid
	}
	complete := false
	defer func() {
		if !complete {
			abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, sink.Abort(abortCtx))
			summary = Summary{}
		}
	}()
	d, err := definition(options.Table)
	if err != nil || !validLimits(options.Limits) || base == nil || options.Scope == "" || len(options.Scope) > 256 || options.SpoolDirectory == "" || options.Quality.MinimumRows < 0 || options.Quality.MaximumDeletedRows < 0 {
		return Summary{}, ErrInvalid
	}
	if input.Scope != options.Scope {
		return Summary{}, ErrScope
	}
	if input.Lease == nil || input.Lease.Reference().RunID == "" {
		return Summary{}, ErrInvalid
	}
	entries := input.Lease.Pages()
	var mode ingest.Mode
	found := false
	for _, entry := range entries {
		if mode == "" {
			mode = entry.Request.Mode
		} else if mode != entry.Request.Mode {
			return Summary{}, ErrScope
		}
		if entry.Request.Table == options.Table {
			found = true
		}
		// Lease.Validate rechecks all fourteen sources, including non-target
		// pages. Bound that work before the validation starts as well.
		if entry.Summary.Count > options.Limits.InputPageRows || entry.PayloadBytes > options.Limits.InputPageBytes {
			return Summary{}, ErrLimit
		}
		if entry.Summary.Count > summary.Buffers.PeakInputPageRows {
			summary.Buffers.PeakInputPageRows = entry.Summary.Count
		}
		if entry.PayloadBytes > summary.Buffers.PeakInputPageBytes {
			summary.Buffers.PeakInputPageBytes = entry.PayloadBytes
		}
	}
	if !found || (mode != ingest.Full && mode != ingest.ForceFull && mode != ingest.Incremental) {
		return Summary{}, ErrInvalid
	}
	bounded, err := input.Lease.Bounded(ctx, options.Limits.InputPageBytes, options.Limits.InputPageRows)
	if err != nil {
		return Summary{}, err
	}
	input.Lease = bounded
	root, err := filepath.Abs(options.SpoolDirectory)
	if err != nil {
		return Summary{}, ErrSpool
	}
	scratch, err := os.MkdirTemp(root, "merge-")
	if err != nil {
		return Summary{}, ErrSpool
	}
	defer os.RemoveAll(scratch) // Only this freshly-created private task directory.
	summary.Input = input.Lease.Reference()
	summary.DeletionsKnown = mode != ingest.Incremental
	s := sorter{ctx: ctx, table: options.Table, root: scratch, limits: options.Limits, stats: &summary.Buffers}
	for {
		if err := ctx.Err(); err != nil {
			return Summary{}, err
		}
		row, err := base.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return Summary{}, err
		}
		entry, err := makeItem(options.Table, row, true)
		if err != nil {
			return Summary{}, err
		}
		if err := s.add(entry); err != nil {
			return Summary{}, err
		}
	}
	for i, entry := range entries {
		if entry.Request.Table != options.Table {
			continue
		}
		page, err := input.Lease.ReadPage(ctx, i)
		if err != nil {
			return Summary{}, err
		}
		if len(page.Records) > summary.Buffers.PeakInputPageRows {
			summary.Buffers.PeakInputPageRows = len(page.Records)
		}
		if entry.PayloadBytes > summary.Buffers.PeakInputPageBytes {
			summary.Buffers.PeakInputPageBytes = entry.PayloadBytes
		}
		for _, row := range page.Records {
			entry, err := makeItem(options.Table, row, false)
			if err != nil {
				return Summary{}, err
			}
			if err := s.add(entry); err != nil {
				return Summary{}, err
			}
		}
	}
	merged, err := s.finish()
	if err != nil {
		return Summary{}, err
	}
	defer merged.close()
	baseHash, targetHash := digestHeader(options.Table, options.Scope), digestHeader(options.Table, options.Scope)
	var output []ingest.Record
	var outputBytes int64
	flush := func() error {
		if len(output) == 0 {
			return nil
		}
		if err := sink.WriteBatch(ctx, output); err != nil {
			return err
		}
		output = nil
		outputBytes = 0
		return nil
	}
	var group keyGroup
	emit := func() error {
		if group.key == "" {
			return nil
		}
		target, err := group.target(d.History, mode)
		if err != nil {
			return err
		}
		if group.before != nil {
			summary.BeforeRows++
			writeDigest(baseHash, group.before.raw)
		}
		if target == nil {
			if group.before != nil {
				summary.Removed++
			}
			return nil
		}
		summary.AfterRows++
		if group.before == nil {
			summary.Added++
		} else if !bytes.Equal(group.before.raw, target.raw) {
			summary.Modified++
		}
		writeDigest(targetHash, target.raw)
		if len(output) == options.Limits.BatchRows || int64(len(target.raw)) > options.Limits.BatchBytes-outputBytes {
			if err := flush(); err != nil {
				return err
			}
		}
		output = append(output, target.row)
		outputBytes += int64(len(target.raw))
		if len(output) > summary.Buffers.PeakOutputRows {
			summary.Buffers.PeakOutputRows = len(output)
		}
		if outputBytes > summary.Buffers.PeakOutputBytes {
			summary.Buffers.PeakOutputBytes = outputBytes
		}
		return nil
	}
	for {
		entry, readErr := merged.next()
		if readErr == io.EOF {
			if err := emit(); err != nil {
				return Summary{}, err
			}
			break
		}
		if readErr != nil {
			return Summary{}, readErr
		}
		if group.key != "" && group.key != entry.row.Key {
			if err := emit(); err != nil {
				return Summary{}, err
			}
			group = keyGroup{}
		}
		if err := group.add(entry, d.History); err != nil {
			return Summary{}, err
		}
	}
	if hex.EncodeToString(baseHash.Sum(nil)) != options.BaselineSHA256 {
		return Summary{}, ErrBaseline
	}
	if summary.DeletionsKnown && (summary.AfterRows < int64(options.Quality.MinimumRows) || summary.Removed > int64(options.Quality.MaximumDeletedRows) || (summary.BeforeRows > 0 && summary.AfterRows == 0 && !options.Quality.AllowEmpty)) {
		return Summary{}, ErrQuality
	}
	if err := flush(); err != nil {
		return Summary{}, err
	}
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	summary.SHA256 = hex.EncodeToString(targetHash.Sum(nil))
	if err := sink.Complete(ctx, summary); err != nil {
		return Summary{}, err
	}
	complete = true
	return summary, nil
}

type keyGroup struct {
	key                      string
	before, winner, previous *item
	incoming, distinct       int64
	invalidTime              bool
}

func (g *keyGroup) add(entry item, history bool) error {
	g.key = entry.row.Key
	copy := entry
	if entry.base {
		if g.before != nil {
			return ErrDuplicate
		}
		g.before = &copy
	} else {
		g.incoming++
		if !history && g.incoming > 1 {
			return ErrDuplicate
		}
	}
	if !history {
		if !entry.base {
			g.winner = &copy
		}
		return nil
	}
	if g.previous == nil || !bytes.Equal(g.previous.raw, entry.raw) {
		g.distinct++
		if !entry.timed {
			g.invalidTime = true
		}
		if g.previous != nil && g.previous.timed && entry.timed && g.previous.stamp.Equal(entry.stamp) {
			return ErrHistoryConflict
		}
	}
	if g.winner == nil || (entry.timed && (!g.winner.timed || entry.stamp.After(g.winner.stamp))) {
		g.winner = &copy
	}
	g.previous = &copy
	return nil
}
func (g *keyGroup) target(history bool, mode ingest.Mode) (*item, error) {
	if history && g.distinct > 1 && g.invalidTime {
		return nil, ErrHistoryTime
	}
	if g.incoming == 0 {
		if mode == ingest.Incremental {
			return g.before, nil
		}
		return nil, nil
	}
	return g.winner, nil
}
