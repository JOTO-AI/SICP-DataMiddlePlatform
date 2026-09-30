package staging

import (
	"context"
	"sync"
)

type verificationKey struct{}
type verificationScope struct {
	mu    sync.RWMutex
	open  bool
	lease *Lease
	bytes int64
	rows  int
}

// WithVerifiedPages shares one bounded full verification across a joined group
// of table builds. Each ReadPage still verifies its payload. A final full check
// catches changes to receipts or any non-target table before the caller can
// publish a target manifest. The scoped shortcut expires on every exit path.
func (l *Lease) WithVerifiedPages(ctx context.Context, bytes int64, rows int, work func(context.Context) error) error {
	if work == nil {
		return ErrInvalid
	}
	bounded, err := l.Bounded(ctx, bytes, rows)
	if err != nil {
		return err
	}
	scope := &verificationScope{open: true, lease: bounded, bytes: bytes, rows: rows}
	defer func() { scope.mu.Lock(); scope.open = false; scope.mu.Unlock() }()
	if err := work(context.WithValue(ctx, verificationKey{}, scope)); err != nil {
		return err
	}
	return bounded.Validate(ctx)
}

func (l *Lease) scopedBounded(ctx context.Context, bytes int64, rows int) (*Lease, bool, error) {
	scope, ok := ctx.Value(verificationKey{}).(*verificationScope)
	if !ok {
		return nil, false, nil
	}
	scope.mu.RLock()
	defer scope.mu.RUnlock()
	if !scope.open {
		return nil, true, ErrInvalid
	}
	b := scope.lease
	if scope.bytes != bytes || scope.rows != rows || l.store.files != b.store.files || l.owner != b.owner || l.Reference() != b.Reference() {
		return nil, false, nil
	}
	q := l.store.files.(*QuotaFiles)
	q.lifecycle.RLock()
	defer q.lifecycle.RUnlock()
	if err := b.active(ctx); err != nil {
		return nil, true, err
	}
	return b, true, nil
}
