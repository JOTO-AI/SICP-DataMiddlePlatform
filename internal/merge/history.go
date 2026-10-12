package merge

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

var (
	ErrHistoryConflict = errors.New("conflicting source history at the same priority time")
	ErrHistoryTime     = errors.New("source history has no comparable priority time")
)

// Latest selects one complete source row per person for the three explicitly
// approved historical sources. Supply both current and incoming source records
// for a cross-round comparison. Identical records may collapse without a time;
// all different versions must have comparable priority times.
//
// No business-update field is currently confirmed for these sources. ETL_TIME
// is therefore the only priority field. It is parsed as a naive local calendar
// value, never lexicographically and never using the worker's current clock.
func Latest(ctx context.Context, table string, rows []ingest.Record) ([]ingest.Record, error) {
	d, err := definition(table)
	if err != nil || !d.History {
		return nil, ErrInvalid
	}
	groups := make(map[string]map[string]ingest.Record)
	for _, r := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		copy, raw, err := validated(ctx, table, r)
		if err != nil {
			return nil, err
		}
		if groups[r.Key] == nil {
			groups[r.Key] = make(map[string]ingest.Record)
		}
		groups[r.Key][string(raw)] = copy
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]ingest.Record, 0, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		versions := groups[key]
		var selected ingest.Record
		var latest time.Time
		times := make(map[time.Time]bool, len(versions))
		for _, r := range versions {
			if len(versions) == 1 {
				selected = r
				break
			}
			priority, err := historyTime(r)
			if err != nil {
				return nil, err
			}
			// Check every time, including older versions which cannot win.
			// Otherwise input order could hide an old same-time conflict.
			if times[priority] {
				return nil, ErrHistoryConflict
			}
			times[priority] = true
			if selected.Key == "" || priority.After(latest) {
				selected = r
				latest = priority
			}
		}
		result = append(result, selected)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func historyTime(r ingest.Record) (time.Time, error) {
	token, present := r.Fields["ETL_TIME"]
	bound, err := value.Bind(value.Timestamp, value.Field{Present: present, Raw: token.Raw})
	if err != nil || bound == nil {
		return time.Time{}, ErrHistoryTime
	}
	parsed, ok := bound.(time.Time)
	if !ok {
		return time.Time{}, ErrHistoryTime
	}
	return parsed, nil
}
