package observability

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

var ErrDelivery = errors.New("log delivery not acknowledged")
var ErrConflict = errors.New("event identity payload conflict")

type Stored struct {
	ID   string
	Body []byte
}

// Journal publishes complete immutable payloads durably, and only removes an
// identity after an explicit delivery acknowledgement. Implementations honor
// contexts and bound every read/list by the supplied limits.
type Journal interface {
	Put(context.Context, string, []byte) error
	Batch(context.Context, int, int) ([]Stored, error)
	Remove(context.Context, string) error
	Flush(context.Context) error
	Lookup(context.Context, string) ([]byte, error)
	Usage(context.Context) (Usage, error)
}
type LogStore interface {
	StoreBatch(context.Context, []SafeEvent) error
}
type Buffer struct {
	files                  *QuotaJournal
	sanitizer              *Sanitizer
	batchCount, batchBytes int
	delivery               chan struct{}
	wake                   chan struct{}
	appendGate             chan struct{}
	closed                 atomic.Bool
	source                 storage.LogSource
}

func NewBuffer(files *QuotaJournal, s *Sanitizer, batchCount, batchBytes int) (*Buffer, error) {
	if files == nil || s == nil || batchCount < 1 || batchCount > 1000 || batchBytes < s.maxBytes || batchBytes > 64<<20 {
		return nil, ErrBuffer
	}
	return &Buffer{files: files, sanitizer: s, batchCount: batchCount, batchBytes: batchBytes, delivery: make(chan struct{}, 1), wake: make(chan struct{}, 1), appendGate: make(chan struct{}, 1)}, nil
}
func (b *Buffer) Append(ctx context.Context, e SafeEvent) error {
	if b == nil || !e.Valid() {
		return ErrPayload
	}
	if b.closed.Load() {
		return ErrClosed
	}
	select {
	case b.appendGate <- struct{}{}:
		defer func() { <-b.appendGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if b.closed.Load() {
		return ErrClosed
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	safe, err := b.sanitizer.restore(e.Bytes())
	if err != nil {
		return err
	}
	if !acceptsSource(b.source, safe) {
		return ErrPayload
	}
	if err := b.files.Put(ctx, safe.ID(), safe.Bytes()); err != nil {
		if errors.Is(err, ErrCapacity) {
			return ErrCapacity
		}
		return ErrBuffer
	}
	select {
	case b.wake <- struct{}{}:
	default:
	}
	return nil
}
func (b *Buffer) Pending(ctx context.Context) ([]SafeEvent, error) {
	if b == nil {
		return nil, ErrBuffer
	}
	stored, err := b.files.Batch(ctx, b.batchCount, b.batchBytes)
	if err != nil {
		return nil, ErrBuffer
	}
	if len(stored) > b.batchCount {
		return nil, ErrLimit
	}
	total := 0
	seen := map[string]bool{}
	result := make([]SafeEvent, 0, len(stored))
	for _, item := range stored {
		if len(item.Body) > b.batchBytes-total {
			return nil, ErrLimit
		}
		total += len(item.Body)
		safe, err := b.sanitizer.restore(item.Body)
		if err != nil || safe.ID() != item.ID || seen[item.ID] || !acceptsSource(b.source, safe) {
			return nil, ErrPayload
		}
		seen[item.ID] = true
		result = append(result, safe)
	}
	return result, nil
}

// Deliver never holds the append/file lock while waiting for the independent
// log database. A lost commit reply leaves every event available for replay.
func (b *Buffer) Deliver(ctx context.Context, store LogStore) (int, error) {
	if b == nil || store == nil {
		return 0, ErrBuffer
	}
	select {
	case b.delivery <- struct{}{}:
		defer func() { <-b.delivery }()
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	batch, err := b.Pending(ctx)
	if err != nil {
		return 0, err
	}
	if len(batch) == 0 {
		return 0, nil
	}
	if err := store.StoreBatch(ctx, batch); err != nil {
		return 0, ErrDelivery
	}
	if ctx.Err() != nil {
		return 0, ErrDelivery
	}
	for n, e := range batch {
		if err := b.files.Remove(ctx, e.ID()); err != nil {
			return n, ErrBuffer
		}
	}
	return len(batch), nil
}
