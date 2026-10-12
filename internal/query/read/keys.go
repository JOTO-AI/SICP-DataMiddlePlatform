package read

import (
	"context"
	"sort"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

// CollectKeys reads exact text-key sets in bounded batches, using one fixed
// handle and the caller's shared budget. Missing associations remain absent.
func CollectKeys(ctx context.Context, r Matcher, h Handle, table, column string, keys []string, b *Budget) ([]source.SourceRow, error) {
	if r == nil || h.Validate() != nil {
		return nil, ErrHandle
	}
	if err := b.Check(ctx); err != nil {
		return nil, err
	}
	pk := ""
	allowed := false
	for _, t := range source.Catalog() {
		if t.Name == table {
			pk = t.PrimaryKey
			for _, c := range t.Columns {
				if c.Name == column && c.Kind == value.Text {
					allowed = true
				}
			}
		}
	}
	if !allowed {
		return nil, source.ErrInput
	}
	unique := map[string]bool{}
	for _, key := range keys {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		if strings.TrimSpace(key) == "" || len(key) > 4096 || strings.ContainsRune(key, 0) {
			return nil, source.ErrInput
		}
		if !unique[key] {
			if err := b.Reserve(len(key) + 64); err != nil {
				return nil, err
			}
			unique[key] = true
		}
	}
	ordered := make([]string, 0, len(unique))
	for key := range unique {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	result := []source.SourceRow{}
	seen := map[string]bool{}
	for start := 0; start < len(ordered); start += source.MaxBatchRows {
		end := start + source.MaxBatchRows
		if end > len(ordered) {
			end = len(ordered)
		}
		chunk := ordered[start:end]
		err := Scan(ctx, r, h, table, []source.Filter{{Column: column, Values: chunk}}, b, func(row source.SourceRow) error {
			key, err := compute.Required(row, column)
			if err != nil {
				return err
			}
			index := sort.SearchStrings(chunk, key)
			if index == len(chunk) || chunk[index] != key {
				return source.ErrInput
			}
			id, err := compute.Required(row, pk)
			if err != nil {
				return err
			}
			if seen[id] {
				return ErrAmbiguous
			}
			if err := b.Reserve(len(id) + 64); err != nil {
				return err
			}
			if err := b.Retain(row); err != nil {
				return err
			}
			seen[id] = true
			result = append(result, row)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if err := b.Check(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// LookupMany requires one row per association key (for example course number,
// which is not necessarily the source primary key). Conflicts fail the query.
func LookupMany(ctx context.Context, r Matcher, h Handle, table, column string, keys []string, b *Budget) (compute.Index, error) {
	rows, err := CollectKeys(ctx, r, h, table, column, keys, b)
	if err != nil {
		return nil, err
	}
	index := compute.Index{}
	for _, row := range rows {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		key, err := compute.Required(row, column)
		if err != nil {
			return nil, err
		}
		if _, exists := index[key]; exists {
			return nil, ErrAmbiguous
		}
		if err := b.Reserve(len(key) + 64); err != nil {
			return nil, err
		}
		index[key] = row
	}
	return index, nil
}
