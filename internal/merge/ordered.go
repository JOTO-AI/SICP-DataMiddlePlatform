package merge

import (
	"context"
	"io"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
)

// Both sides use less, including historical timestamp/raw tie ordering. The
// existing keyGroup remains authoritative for replacement/conflict semantics.
type orderedJoin struct {
	ctx                 context.Context
	table, previous     string
	base                RowIterator
	delta               *runMerge
	baseHead, deltaHead *item
	baseDone, deltaDone bool
	maxBytes, buffered  int64
}

func (j *orderedJoin) close() { buildmetric.Memory(j.ctx, -j.buffered); j.buffered = 0 }
func (j *orderedJoin) next() (item, error) {
	if err := j.ctx.Err(); err != nil {
		return item{}, err
	}
	if j.baseHead == nil && !j.baseDone {
		j.close()
		row, err := j.base.Next(j.ctx)
		if err == io.EOF {
			j.baseDone = true
		} else if err != nil {
			return item{}, err
		} else {
			entry, err := makeItem(j.ctx, j.table, row, true)
			if err != nil {
				return item{}, err
			}
			if entry.row.Key <= j.previous {
				return item{}, ErrBaseline
			}
			if int64(len(entry.raw)) > j.maxBytes {
				return item{}, ErrLimit
			}
			j.previous = entry.row.Key
			j.baseHead = &entry
			j.buffered = int64(len(entry.raw))
			buildmetric.Memory(j.ctx, j.buffered)
		}
	}
	if j.deltaHead == nil && !j.deltaDone {
		entry, err := j.delta.next()
		if err == io.EOF {
			j.deltaDone = true
		} else if err != nil {
			return item{}, err
		} else {
			j.deltaHead = &entry
		}
	}
	if j.baseHead == nil && j.deltaHead == nil {
		return item{}, io.EOF
	}
	if j.baseHead != nil && (j.deltaHead == nil || less(*j.baseHead, *j.deltaHead)) {
		out := *j.baseHead
		j.baseHead = nil
		return out, nil
	}
	out := *j.deltaHead
	j.deltaHead = nil
	return out, nil
}
