// Package merge compares and merges validated source records without converting
// them through SQL values. Missing keys and explicit null must remain distinct.
package merge

import (
	"bytes"
	"context"
	"errors"
	"sort"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

var ErrInvalid = errors.New("invalid merge source evidence")

type ChangeKind string

const (
	Added    ChangeKind = "added"
	Removed  ChangeKind = "removed"
	Modified ChangeKind = "modified"
)

// Change describes a source multiset difference, not a physical write count or
// an inferred upstream deletion. Incremental deletion remains unknown.
type Change struct {
	Key                   string
	Kind                  ChangeKind
	BeforeRows, AfterRows int
}

type Report struct {
	BeforeRows, AfterRows int
	Changes               []Change
}

func definition(table string) (ingest.Definition, error) {
	for _, d := range ingest.Catalog() {
		if d.Name == table {
			return d, nil
		}
	}
	return ingest.Definition{}, ErrInvalid
}

// validated detaches caller-owned tokens and checks the declared key against
// the source object. A manually constructed Record cannot bypass source checks.
func validated(table string, r ingest.Record) (ingest.Record, []byte, error) {
	raw, err := r.Canonical()
	if err != nil {
		return ingest.Record{}, nil, ErrInvalid
	}
	copy, err := ingest.DecodeRecord(table, raw)
	if err != nil || copy.Key != r.Key {
		return ingest.Record{}, nil, ErrInvalid
	}
	return copy, raw, nil
}

// Compare compares bounded input batches as multisets, including duplicate row
// multiplicity. It does not collapse historical rows; latest-row selection is
// a separate operation. Changes are sorted by the exact source primary key.
func Compare(ctx context.Context, table string, before, after []ingest.Record) (Report, error) {
	if _, err := definition(table); err != nil {
		return Report{}, err
	}
	group := func(rows []ingest.Record) (map[string][][]byte, error) {
		result := make(map[string][][]byte)
		for _, r := range rows {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			_, raw, err := validated(table, r)
			if err != nil {
				return nil, err
			}
			result[r.Key] = append(result[r.Key], raw)
		}
		for _, rows := range result {
			sort.Slice(rows, func(i, j int) bool { return bytes.Compare(rows[i], rows[j]) < 0 })
		}
		return result, nil
	}
	old, err := group(before)
	if err != nil {
		return Report{}, err
	}
	new, err := group(after)
	if err != nil {
		return Report{}, err
	}
	keys := make([]string, 0, len(old)+len(new))
	for key := range old {
		keys = append(keys, key)
	}
	for key := range new {
		if _, exists := old[key]; !exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	report := Report{BeforeRows: len(before), AfterRows: len(after), Changes: []Change{}}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		a, b := old[key], new[key]
		equal := len(a) == len(b)
		if equal {
			for i := range a {
				if !bytes.Equal(a[i], b[i]) {
					equal = false
					break
				}
			}
		}
		if equal {
			continue
		}
		kind := Modified
		if len(a) == 0 {
			kind = Added
		} else if len(b) == 0 {
			kind = Removed
		}
		report.Changes = append(report.Changes, Change{Key: key, Kind: kind, BeforeRows: len(a), AfterRows: len(b)})
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	return report, nil
}
