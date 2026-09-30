package candidate

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const MaxTableConcurrency = 4

// prepareTables joins every writer before returning. Results retain catalog
// order, and the immutable manifest is written only after all tables validate.
func (f TargetFiles) prepareTables(ctx context.Context, o PrepareOptions, input *staging.Lease, base map[string]TableVersion) ([]targetTable, map[string]ChangeCounts, error) {
	concurrency := o.TableConcurrency
	if concurrency == 0 {
		concurrency = 1
	}
	if concurrency < 1 || concurrency > MaxTableConcurrency {
		return nil, nil, ErrBuild
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	catalog := source.Catalog()
	tables := make([]targetTable, len(catalog))
	counts := make([]ChangeCounts, len(catalog))
	var next, chunks atomic.Int64
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	var activeMu sync.Mutex
	active := map[int]bool{}
	report := func(index int, running bool) {
		activeMu.Lock()
		defer activeMu.Unlock()
		if running {
			active[index] = true
		} else {
			delete(active, index)
		}
		var names []string
		for i, t := range catalog {
			if active[i] {
				names = append(names, t.Name)
			}
		}
		if len(names) > 0 {
			activity.ReportTables(ctx, "building_table", names)
		}
	}
	reserve := func() bool { return chunks.Add(1) <= int64(f.MaxChunks) }
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				index := int(next.Add(1) - 1)
				if index >= len(catalog) {
					return
				}
				report(index, true)
				table, count, err := f.prepareTable(ctx, o, input, base, catalog[index].Name, reserve)
				if err != nil {
					once.Do(func() { firstErr = err; cancel() })
					return
				}
				tables[index], counts[index] = table, count
				report(index, false)
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	out := map[string]ChangeCounts{}
	for i, t := range catalog {
		out[t.Name] = counts[i]
	}
	return tables, out, nil
}

func (f TargetFiles) prepareTable(ctx context.Context, o PrepareOptions, input *staging.Lease, base map[string]TableVersion, name string, reserve func() bool) (result targetTable, changes ChangeCounts, err error) {
	end := events.BeginBuild(ctx, events.BuildScope{Stage: "merge_table", Table: name, Combination: o.Combination}, "")
	defer func() { end(err) }()
	var current merge.RowIterator = emptyRows{}
	baseDigest, err := merge.SourceDigest(ctx, name, o.Scope, nil)
	if err != nil {
		return targetTable{}, ChangeCounts{}, err
	}
	var opened Rows
	if old, ok := base[name]; ok {
		opened, err = o.BaseSource.Open(ctx, *o.Base, old)
		if err != nil {
			return targetTable{}, ChangeCounts{}, err
		}
		if opened == nil {
			return targetTable{}, ChangeCounts{}, ErrBuild
		}
		current = opened
		baseDigest = old.SHA256
	}
	sink := &fileSink{files: f, combination: o.Combination, table: name, remaining: f.MaxChunks, reserve: reserve}
	summary, err := merge.Stream(ctx, merge.StreamOptions{Table: name, Scope: o.Scope, BaselineSHA256: baseDigest, SpoolDirectory: o.SpoolDirectory, Limits: o.Limits, Quality: o.Quality[name]}, current, merge.SealedInput{Scope: o.Scope, Lease: input}, sink)
	if opened != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = errors.Join(err, opened.Close(cleanup))
		cancel()
	}
	if err != nil {
		return targetTable{}, ChangeCounts{}, err
	}
	if !sink.complete || summary.Input != input.Reference() {
		return targetTable{}, ChangeCounts{}, ErrBuild
	}
	version := TableVersion{Table: name, Scope: o.Scope, SHA256: summary.SHA256, Rows: summary.AfterRows}
	return targetTable{Version: version, Chunks: sink.chunks}, ChangeCounts{Before: summary.BeforeRows, After: summary.AfterRows, Added: summary.Added, Modified: summary.Modified, Removed: summary.Removed, DeletionsKnown: summary.DeletionsKnown}, nil
}
