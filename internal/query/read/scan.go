package read

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrBudget = errors.New("query resource budget exceeded")
var ErrOrder = errors.New("invalid source cursor order")

type Limits struct {
	Batches, Rows, Bytes, BatchSize int
	Duration                        time.Duration
	// BatchBytes enables streaming accounting; Bytes then bounds retained
	// Collect payloads instead of cumulative traffic across streaming scans.
	BatchBytes int
}

func DefaultLimits() Limits {
	return Limits{Batches: 1000, Rows: 100000, Bytes: 64 << 20, BatchSize: 500, Duration: 20 * time.Second}
}

// Budget is owned by one synchronous request and shared across all its scans.
// The monotonic deadline and parent context both apply; no background read is
// abandoned on timeout. The database adapter must honor the derived deadline.
type Budget struct {
	limits               Limits
	clock                ports.Clock
	started              time.Time
	batches, rows, bytes int
	retained             int
}

func NewBudget(l Limits, clock ports.Clock) (*Budget, error) {
	if clock == nil || l.Batches < 1 || l.Rows < 1 || l.Bytes < 1 || l.BatchSize < 1 || l.BatchSize > source.MaxBatchRows || l.Duration <= 0 || l.Duration > time.Hour || l.BatchBytes < 0 || l.BatchBytes > 64<<20 {
		return nil, ErrBudget
	}
	return &Budget{limits: l, clock: clock, started: clock.Now()}, nil
}
func (b *Budget) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil {
		return ErrBudget
	}
	now := b.clock.Now()
	if now.Before(b.started) || now.Sub(b.started) > b.limits.Duration {
		return ErrBudget
	}
	return nil
}

// Scan visits every source row in key order, even if the consumer rejects all
// rows in an earlier batch. An error discards collected output at the caller.
// No SELECT LIMIT derived from the business page may be used as completeness.
func Scan(ctx context.Context, r Matcher, h Handle, table string, filters []source.Filter, b *Budget, visit func(source.SourceRow) error) error {
	if r == nil || visit == nil || h.Validate() != nil {
		return ErrHandle
	}
	if err := b.Check(ctx); err != nil {
		return err
	}
	pk := ""
	for _, t := range source.Catalog() {
		if t.Name == table {
			pk = t.PrimaryKey
			break
		}
	}
	if pk == "" {
		return source.ErrInput
	}
	remaining := b.limits.Duration - b.clock.Now().Sub(b.started)
	// Keep the fake clock authoritative for equality at the budget boundary;
	// a minimum real deadline still propagates cancellation to SQL adapters.
	if remaining <= 0 {
		remaining = time.Nanosecond
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	after := ""
	var buffered int64
	defer func() { buildmetric.Memory(ctx, -buffered) }()
	for {
		buildmetric.Memory(ctx, -buffered)
		buffered = 0
		if err := b.Check(ctx); err != nil {
			return err
		}
		if b.batches >= b.limits.Batches {
			return ErrBudget
		}
		limit := b.limits.BatchSize
		// Probe at most one beyond the row budget, never claim truncated success.
		if left := b.limits.Rows - b.rows; left < limit {
			limit = left + 1
		}
		b.batches++
		rows, err := r.ReadMatching(ctx, h, table, after, limit, filters)
		if err != nil {
			return err
		}
		if buildmetric.Enabled(ctx) {
			for _, row := range rows {
				for _, raw := range row {
					buffered += int64(len(raw))
				}
			}
		}
		buildmetric.Memory(ctx, buffered)
		if err = b.Check(ctx); err != nil {
			return err
		}
		if len(rows) > limit {
			return ErrBudget
		}
		batchBytes := 0
		for _, row := range rows {
			if b.rows >= b.limits.Rows {
				return ErrBudget
			}
			b.rows++
			for name, raw := range row {
				used, maximum := &b.bytes, b.limits.Bytes
				if b.limits.BatchBytes > 0 {
					used, maximum = &batchBytes, b.limits.BatchBytes
				}
				if len(name) > maximum-*used {
					return ErrBudget
				}
				*used += len(name)
				if len(raw) > maximum-*used {
					return ErrBudget
				}
				*used += len(raw)
			}
			var key string
			if json.Unmarshal(row[pk], &key) != nil || key == "" || key <= after {
				return ErrOrder
			}
			after = key
			if err = visit(row); err != nil {
				return err
			}
			if err = b.Check(ctx); err != nil {
				return err
			}
		}
		if len(rows) < limit {
			return nil
		}
	}
}

func Collect(ctx context.Context, r Matcher, h Handle, table string, filters []source.Filter, b *Budget) ([]source.SourceRow, error) {
	result := []source.SourceRow{}
	err := Scan(ctx, r, h, table, filters, b, func(row source.SourceRow) error {
		if err := b.Retain(row); err != nil {
			return err
		}
		result = append(result, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Retain accounts for filtered rows held after a streaming callback returns.
// Foreground Scan already charges all traffic, so its stricter cap is unchanged.
func (b *Budget) Retain(row source.SourceRow) error {
	if b == nil {
		return ErrBudget
	}
	if b.limits.BatchBytes == 0 {
		return nil
	}
	for name, raw := range row {
		if len(name) > b.limits.Bytes-b.retained {
			return ErrBudget
		}
		b.retained += len(name)
		if len(raw) > b.limits.Bytes-b.retained {
			return ErrBudget
		}
		b.retained += len(raw)
	}
	return nil
}

// Reserve charges retained association keys and indexes in the same request
// budget as source reads. Call before allocating the corresponding entry.
func (b *Budget) Reserve(size int) error {
	if b == nil || size < 0 {
		return ErrBudget
	}
	used := &b.bytes
	if b.limits.BatchBytes > 0 {
		used = &b.retained
	}
	if size > b.limits.Bytes-*used {
		return ErrBudget
	}
	*used += size
	return nil
}
