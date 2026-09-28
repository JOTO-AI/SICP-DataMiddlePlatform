package staging

import (
	"context"
	"errors"
	"sync"
)

// QuotaFiles must be shared by every Store using the same staging directory.
// Temporary, orphaned, payload and receipt bytes all count. The execution
// ownership protocol provides the single worker process writing that volume.
type QuotaFiles struct {
	files     Files
	limit     int64
	mu        sync.Mutex
	lifecycle sync.Mutex // Retain/release/retirement share this across all Stores.
}

func NewQuotaFiles(files Files, limit int64) (*QuotaFiles, error) {
	if files == nil || limit < 1 {
		return nil, ErrInvalid
	}
	return &QuotaFiles{files: files, limit: limit}, nil
}
func (q *QuotaFiles) WriteTemporary(ctx context.Context, data []byte) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	used, err := q.files.Usage(ctx)
	if err != nil {
		return "", fileError(err)
	}
	if used < 0 || used > q.limit || int64(len(data)) > q.limit-used {
		return "", ErrCapacity
	}
	return q.files.WriteTemporary(ctx, data)
}
func (q *QuotaFiles) Read(ctx context.Context, name string, limit int64) ([]byte, error) {
	return q.files.Read(ctx, name, limit)
}
func (q *QuotaFiles) Publish(ctx context.Context, temp, name string) error {
	return q.files.Publish(ctx, temp, name)
}
func (q *QuotaFiles) Sync(ctx context.Context) error { return q.files.Sync(ctx) }
func (q *QuotaFiles) List(ctx context.Context, prefix string) ([]string, error) {
	return q.files.List(ctx, prefix)
}
func (q *QuotaFiles) Usage(ctx context.Context) (int64, error) { return q.files.Usage(ctx) }

// RemoveRetired is for a caller holding committed control retirement and
// exclusive worker ownership. It does not establish collection eligibility.
func (q *QuotaFiles) RemoveRetired(ctx context.Context, name string) error {
	return q.remove(ctx, name)
}

func (q *QuotaFiles) remove(ctx context.Context, name string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	remover, ok := q.files.(interface {
		Remove(context.Context, string) error
	})
	if !ok {
		return ErrStorage
	}
	return remover.Remove(ctx, name)
}
func fileError(err error) error {
	if errors.Is(err, ErrCapacity) {
		return ErrCapacity
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrStorage
}
