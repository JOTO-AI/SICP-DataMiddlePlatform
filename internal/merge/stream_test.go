package merge_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

type rowIterator struct {
	rows  []ingest.Record
	index int
	end   func() error
}

func (r *rowIterator) Next(ctx context.Context) (ingest.Record, error) {
	if err := ctx.Err(); err != nil {
		return ingest.Record{}, err
	}
	if r.index == len(r.rows) {
		if r.end != nil {
			if err := r.end(); err != nil {
				return ingest.Record{}, err
			}
		}
		return ingest.Record{}, io.EOF
	}
	row := r.rows[r.index]
	r.index++
	return row, nil
}

type candidate struct {
	rows                    []ingest.Record
	complete, aborted       bool
	writes, maxRows         int
	maxBytes                int64
	failWrite, failComplete bool
}

func (c *candidate) WriteBatch(_ context.Context, rows []ingest.Record) error {
	c.writes++
	if c.failWrite {
		return errors.New("synthetic candidate write failed")
	}
	var size int64
	for _, row := range rows {
		raw, err := row.Canonical()
		if err != nil {
			return err
		}
		size += int64(len(raw))
		// Simulate durable consumption without sharing the caller's batch.
		copy := ingest.Record{Key: row.Key, Fields: make(map[string]ingest.Token, len(row.Fields))}
		for name, token := range row.Fields {
			token.Raw = append([]byte(nil), token.Raw...)
			copy.Fields[name] = token
		}
		c.rows = append(c.rows, copy)
	}
	if len(rows) > c.maxRows {
		c.maxRows = len(rows)
	}
	if size > c.maxBytes {
		c.maxBytes = size
	}
	return nil
}
func (c *candidate) Complete(_ context.Context, _ merge.Summary) error {
	if c.failComplete {
		return errors.New("synthetic complete failed")
	}
	c.complete = true
	return nil
}
func (c *candidate) Abort(context.Context) error {
	c.aborted = true
	c.complete = false
	c.rows = nil
	return nil
}

func streamSetup(t *testing.T, table string, mode ingest.Mode, current, incoming []ingest.Record) (merge.StreamOptions, merge.SealedInput, *candidate) {
	t.Helper()
	store, sealed, _ := sealedStore(t, table, mode, incoming)
	lease, err := store.Retain(context.Background(), sealed.Reference(), "build-m")
	if err != nil {
		t.Fatal(err)
	}
	b := baseline(t, table, current)
	options := merge.StreamOptions{Table: table, Scope: b.Scope, BaselineSHA256: b.SHA256, SpoolDirectory: t.TempDir(), Limits: merge.Limits{BatchRows: 3, BatchBytes: 256, FanIn: 2, MaxRuns: 1000, MaxSpoolBytes: 16 << 20, InputPageRows: 2, InputPageBytes: 1 << 20}, Quality: merge.Quality{MaximumDeletedRows: 1000}}
	return options, merge.SealedInput{Scope: b.Scope, Lease: lease}, &candidate{}
}

func TestUnlimitedSpoolRetainsBoundedBatchesInEveryUpdateMode(t *testing.T) {
	incoming := records(t, grades, `{"ID":"01"}`, `{"ID":"02"}`, `{"ID":"03"}`, `{"ID":"04"}`)
	for _, mode := range []ingest.Mode{ingest.Full, ingest.ForceFull, ingest.Incremental} {
		t.Run(string(mode), func(t *testing.T) {
			o, input, sink := streamSetup(t, grades, mode, nil, incoming)
			o.Limits.MaxSpoolBytes = 0
			o.Limits.BatchRows = 1
			summary, err := merge.Stream(context.Background(), o, &rowIterator{}, input, sink)
			if err != nil || !sink.complete || summary.AfterRows != 4 || summary.Buffers.SpoolBytesWritten <= 0 {
				t.Fatal("unlimited spool failed", err)
			}
			assertRows(t, grades, sink.rows, incoming)
			if sink.maxRows != 1 || summary.Buffers.PeakSortRows > 1 || summary.Buffers.PeakSortBytes > o.Limits.BatchBytes {
				t.Fatal("disabled disk quota removed buffer bounds")
			}
			o, input, sink = streamSetup(t, grades, mode, nil, incoming)
			o.Limits.MaxSpoolBytes = -1
			if _, err := merge.Stream(context.Background(), o, &rowIterator{}, input, sink); err == nil || sink.complete {
				t.Fatal("negative spool budget accepted")
			}
		})
	}
}

func TestT04_14ManyBatchesMatchIndependentExpectedAndBoundBuffers(t *testing.T) {
	var current, incoming, expected []ingest.Record
	for i := 96; i >= 0; i-- {
		current = append(current, records(t, grades, fmt.Sprintf(`{"ID":"%03d","XF":1}`, i))...)
	}
	for i := 112; i >= 0; i-- {
		if i >= 97 || i%2 == 0 {
			value := 2
			if i >= 97 {
				value = 3
			}
			incoming = append(incoming, records(t, grades, fmt.Sprintf(`{"ID":"%03d","XF":%d}`, i, value))...)
		}
	}
	for i := 0; i <= 112; i++ {
		value := 1
		if i >= 97 {
			value = 3
		} else if i%2 == 0 {
			value = 2
		}
		expected = append(expected, records(t, grades, fmt.Sprintf(`{"ID":"%03d","XF":%d}`, i, value))...)
	}
	options, input, sink := streamSetup(t, grades, ingest.Incremental, current, incoming)
	summary, err := merge.Stream(context.Background(), options, &rowIterator{rows: current}, input, sink)
	if err != nil {
		t.Fatal(err)
	}
	if !sink.complete || sink.aborted || sink.writes < 30 {
		t.Fatal("multi-batch candidate did not complete")
	}
	assertRows(t, grades, sink.rows, expected)
	for i, row := range sink.rows {
		if row.Key != expected[i].Key {
			t.Fatal("output order unstable")
		}
	}
	if summary.BeforeRows != 97 || summary.AfterRows != 113 || summary.Added != 16 || summary.Modified != 49 || summary.Removed != 0 || summary.DeletionsKnown {
		t.Fatalf("wrong independent counters: %+v", summary)
	}
	digest, err := merge.SourceDigest(context.Background(), grades, options.Scope, expected)
	if err != nil || digest != summary.SHA256 {
		t.Fatal("target source evidence differs")
	}
	b := summary.Buffers
	if b.PeakSortRows > 3 || b.PeakSortBytes > 256 || b.PeakMergeReaders > 2 || b.PeakOutputRows > 3 || b.PeakOutputBytes > 256 || b.PeakInputPageRows > 2 || b.PeakInputPageBytes > 1<<20 || b.PeakRuns < 3 || sink.maxRows > 3 || sink.maxBytes > 256 {
		t.Fatalf("unbounded buffers: %+v", b)
	}
	entries, err := os.ReadDir(options.SpoolDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatal("scratch directory not cleaned")
	}
}

func TestT04_14StreamingFullDeletesAndIncrementalPreservesMissingRows(t *testing.T) {
	current := records(t, grades, `{"ID":"01","XH":"old"}`, `{"ID":"02"}`, `{"ID":"03"}`)
	incoming := records(t, grades, `{"ID":"01","XH":null}`, `{"ID":"04"}`)
	for _, mode := range []ingest.Mode{ingest.Full, ingest.ForceFull, ingest.Incremental} {
		options, input, sink := streamSetup(t, grades, mode, current, incoming)
		got, err := merge.Stream(context.Background(), options, &rowIterator{rows: current}, input, sink)
		if err != nil {
			t.Fatal(err)
		}
		want := records(t, grades, `{"ID":"01","XH":null}`, `{"ID":"04"}`)
		if mode == ingest.Incremental {
			want = records(t, grades, `{"ID":"01","XH":null}`, `{"ID":"02"}`, `{"ID":"03"}`, `{"ID":"04"}`)
			if got.Removed != 0 || got.DeletionsKnown {
				t.Fatal("incremental claimed deletions")
			}
		} else if got.Removed != 2 || !got.DeletionsKnown {
			t.Fatal("full failed deletion")
		}
		assertRows(t, grades, sink.rows, want)
	}
}

func TestT04_14HistoryAcrossManyRunsUsesConstantGroupStateAndDetectsOldConflict(t *testing.T) {
	for _, d := range historyTables {
		var incoming []ingest.Record
		for i := 59; i >= 0; i-- {
			raw := historyRow(d.key, d.field, fmt.Sprintf(`"v-%02d"`, i), fmt.Sprintf(`"2026-09-01 01:%02d:00"`, i))
			incoming = append(incoming, records(t, d.table, raw)...)
		}
		options, input, sink := streamSetup(t, d.table, ingest.Incremental, nil, incoming)
		options.Limits.BatchBytes = 512
		got, err := merge.Stream(context.Background(), options, &rowIterator{}, input, sink)
		if err != nil {
			t.Fatal(err)
		}
		assertRows(t, d.table, sink.rows, records(t, d.table, historyRow(d.key, d.field, `"v-59"`, `"2026-09-01 01:59:00"`)))
		if got.AfterRows != 1 || got.Buffers.PeakSortRows > 3 || got.Buffers.PeakMergeReaders > 2 {
			t.Fatal("history group was not bounded")
		}
		// Conflict at 01:00 must fail even after a much newer record exists.
		incoming = append(incoming, records(t, d.table, historyRow(d.key, d.field, `"conflict"`, `"2026-09-01 01:00:00"`))...)
		options, input, sink = streamSetup(t, d.table, ingest.Incremental, nil, incoming)
		options.Limits.BatchBytes = 512
		if _, err := merge.Stream(context.Background(), options, &rowIterator{}, input, sink); !errors.Is(err, merge.ErrHistoryConflict) || sink.complete || !sink.aborted {
			t.Fatal("cross-run history conflict completed")
		}
	}
}

func TestT04_14StreamingFailuresNeverCompleteCandidate(t *testing.T) {
	current := records(t, grades, `{"ID":"01"}`, `{"ID":"02"}`, `{"ID":"03"}`, `{"ID":"04"}`)
	incoming := records(t, grades, `{"ID":"01","XF":3}`)
	for _, name := range []string{"baseline", "scope", "quality", "disk budget", "run budget", "row bytes", "page rows", "page bytes", "source read", "duplicate baseline", "candidate write", "candidate complete", "cancel", "released", "no lease"} {
		t.Run(name, func(t *testing.T) {
			options, input, sink := streamSetup(t, grades, ingest.Full, current, incoming)
			iterator := &rowIterator{rows: current}
			ctx := context.Background()
			switch name {
			case "baseline":
				options.BaselineSHA256 = strings.Repeat("0", 64)
			case "scope":
				input.Scope = "another-source"
			case "quality":
				options.Quality.MaximumDeletedRows = 0
			case "disk budget":
				options.Limits.MaxSpoolBytes = 1
			case "run budget":
				options.Limits.MaxRuns = 1
				options.Limits.BatchRows = 1
			case "row bytes":
				options.Limits.BatchBytes = 1
			case "page rows":
				options, input, sink = streamSetup(t, grades, ingest.Full, current, records(t, grades, `{"ID":"01"}`, `{"ID":"02"}`))
				options.Limits.InputPageRows = 1
			case "page bytes":
				options.Limits.InputPageBytes = 1
			case "source read":
				iterator.end = func() error { return errors.New("synthetic baseline failure") }
			case "duplicate baseline":
				iterator.rows = append(append([]ingest.Record(nil), current...), current[0])
			case "candidate write":
				sink.failWrite = true
			case "candidate complete":
				sink.failComplete = true
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "released":
				if err := input.Lease.Release(ctx); err != nil {
					t.Fatal(err)
				}
			case "no lease":
				input.Lease = nil
			}
			result, err := merge.Stream(ctx, options, iterator, input, sink)
			if err == nil || sink.complete || !sink.aborted || !reflect.DeepEqual(result, merge.Summary{}) {
				t.Fatalf("failed candidate escaped gate: %+v %v", result, err)
			}
		})
	}
}

func TestT04_14ScratchCorruptionCannotChangeTargetSilently(t *testing.T) {
	current := records(t, grades, `{"ID":"01","XF":1}`, `{"ID":"02","XF":1}`, `{"ID":"03","XF":1}`)
	options, input, sink := streamSetup(t, grades, ingest.Incremental, current, nil)
	options.Limits.BatchRows = 1
	iterator := &rowIterator{rows: current, end: func() error {
		dirs, err := os.ReadDir(options.SpoolDirectory)
		if err != nil || len(dirs) != 1 {
			t.Fatal("missing scratch directory")
		}
		runs, err := os.ReadDir(filepath.Join(options.SpoolDirectory, dirs[0].Name()))
		if err != nil || len(runs) < 1 {
			t.Fatal("no spilled run")
		}
		path := filepath.Join(options.SpoolDirectory, dirs[0].Name(), runs[0].Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		changed := strings.Replace(string(raw), `"XF":1`, `"XF":9`, 1)
		if changed == string(raw) {
			t.Fatal("fault did not change valid source token")
		}
		return os.WriteFile(path, []byte(changed), 0600)
	}}
	if _, err := merge.Stream(context.Background(), options, iterator, input, sink); !errors.Is(err, merge.ErrSpool) || sink.complete || !sink.aborted {
		t.Fatalf("scratch integrity not checked: %v", err)
	}
}

func TestT04_14NonTargetPagesCannotBypassValidationPageBounds(t *testing.T) {
	for _, bytesLimit := range []bool{false, true} {
		incoming := records(t, grades, `{"ID":"01"}`)
		campus := "dwd_xqjbsjzlb"
		other := records(t, campus, `{"XQH":"01"}`, `{"XQH":"02"}`)
		if bytesLimit {
			other = records(t, campus, fmt.Sprintf(`{"XQH":"01","XQMC":"%s"}`, strings.Repeat("x", 512)))
		}
		store, sealed, _ := sealedStore(t, grades, ingest.Full, incoming, map[string][]ingest.Record{campus: other})
		lease, err := store.Retain(context.Background(), sealed.Reference(), "build-m")
		if err != nil {
			t.Fatal(err)
		}
		options, _, sink := streamSetup(t, grades, ingest.Full, nil, incoming)
		if bytesLimit {
			options.Limits.InputPageBytes = 128
		} else {
			options.Limits.InputPageRows = 1
		}
		if _, err := merge.Stream(context.Background(), options, &rowIterator{}, merge.SealedInput{Scope: options.Scope, Lease: lease}, sink); !errors.Is(err, merge.ErrLimit) || !sink.aborted || sink.complete {
			t.Fatalf("non-target page escaped limits: %v", err)
		}
	}
}

func TestT04_15BothBuildsAndRecoveryUseTheSameRetainedSealedInput(t *testing.T) {
	current := records(t, grades, `{"ID":"01","XF":1}`)
	incoming := records(t, grades, `{"ID":"01","XF":2}`)
	store, sealed, _ := sealedStore(t, grades, ingest.Incremental, incoming)
	var summaries []merge.Summary
	var leases []*staging.Lease
	for _, owner := range []string{"build-m", "build-s", "recovery"} {
		l, err := store.Retain(context.Background(), sealed.Reference(), owner)
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, l)
	}
	base := baseline(t, grades, current)
	for _, lease := range leases {
		options := merge.StreamOptions{Table: grades, Scope: base.Scope, BaselineSHA256: base.SHA256, SpoolDirectory: t.TempDir(), Limits: merge.Limits{BatchRows: 1, BatchBytes: 256, FanIn: 2, MaxRuns: 10, MaxSpoolBytes: 1 << 20, InputPageRows: 2, InputPageBytes: 1 << 20}}
		sink := &candidate{}
		summary, err := merge.Stream(context.Background(), options, &rowIterator{rows: current}, merge.SealedInput{Scope: base.Scope, Lease: lease}, sink)
		if err != nil {
			t.Fatal(err)
		}
		summaries = append(summaries, summary)
		assertRows(t, grades, sink.rows, incoming)
		if err := store.Cleanup(context.Background(), sealed.Reference()); !errors.Is(err, staging.ErrReferenced) {
			t.Fatal("retained build input cleaned")
		}
		if err := lease.Release(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, summary := range summaries {
		if summary.Input != sealed.Reference() || summary.SHA256 != summaries[0].SHA256 {
			t.Fatal("build/recovery source diverged")
		}
	}
	if err := store.Cleanup(context.Background(), sealed.Reference()); err != nil {
		t.Fatal(err)
	}
}
