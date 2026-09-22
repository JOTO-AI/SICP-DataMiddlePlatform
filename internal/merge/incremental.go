package merge

import (
	"context"
	"errors"
	"sort"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

var ErrDuplicate = errors.New("duplicate source primary key")

// Result is a candidate source table, not publication approval. Rows are
// detached from inputs, sorted by the exact source primary key. Changes describe
// source content; they are not database write counts.
type Result struct {
	Rows           []ingest.Record
	Changes        Report
	DeletionsKnown bool
}

func unique(ctx context.Context, table string, rows []ingest.Record) (map[string]ingest.Record, error) {
	byKey := make(map[string]ingest.Record, len(rows))
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		copy, _, err := validated(table, row)
		if err != nil {
			return nil, err
		}
		if _, exists := byKey[row.Key]; exists {
			return nil, ErrDuplicate
		}
		byKey[row.Key] = copy
	}
	return byKey, nil
}

func ordered(rows map[string]ingest.Record) []ingest.Record {
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]ingest.Record, 0, len(keys))
	for _, key := range keys {
		result = append(result, rows[key])
	}
	return result
}

// Incremental replaces entire rows, never individual fields. Missing incoming
// keys retain their previous rows: an incremental source cannot prove deletions.
// current must contain unique source-semantic evidence for the current table,
// not rows reconstructed from SQL NULL values. Input batches must be bounded;
// the large-table streaming entry point applies the same rules separately.
func Incremental(ctx context.Context, table string, current, incoming []ingest.Record) (Result, error) {
	d, err := definition(table)
	if err != nil {
		return Result{}, err
	}
	base, err := unique(ctx, table, current)
	if err != nil {
		return Result{}, err
	}
	var result []ingest.Record
	if d.History {
		all := make([]ingest.Record, 0, len(current)+len(incoming))
		all = append(all, current...)
		all = append(all, incoming...)
		result, err = Latest(ctx, table, all)
		if err != nil {
			return Result{}, err
		}
	} else {
		changes, err := unique(ctx, table, incoming)
		if err != nil {
			return Result{}, err
		}
		for key, row := range changes {
			base[key] = row
		}
		result = ordered(base)
	}
	report, err := Compare(ctx, table, current, result)
	if err != nil {
		return Result{}, err
	}
	return Result{Rows: result, Changes: report, DeletionsKnown: false}, nil
}
