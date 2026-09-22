package observability_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type journal struct {
	mu                  sync.Mutex
	files               map[string][]byte
	failPut, failRemove bool
	flush               func(context.Context) error
}

func newJournal() *journal { return &journal{files: map[string][]byte{}} }
func (j *journal) Put(ctx context.Context, id string, b []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failPut {
		return syscall.ENOSPC
	}
	if old, ok := j.files[id]; ok && string(old) != string(b) {
		return observability.ErrConflict
	}
	j.files[id] = append([]byte(nil), b...)
	return ctx.Err()
}
func (j *journal) Lookup(ctx context.Context, id string) ([]byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	b, ok := j.files[id]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), b...), ctx.Err()
}
func (j *journal) Usage(ctx context.Context) (observability.Usage, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	u := observability.Usage{Files: int64(len(j.files))}
	for _, b := range j.files {
		u.Bytes += int64(len(b))
	}
	return u, ctx.Err()
}
func quota(t *testing.T, j observability.Journal) *observability.QuotaJournal {
	t.Helper()
	q, err := observability.NewQuota(j, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 1000, WarningPercent: 80, Policy: observability.RejectNew})
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func (j *journal) Batch(ctx context.Context, count, limit int) ([]observability.Stored, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var names []string
	for id := range j.files {
		names = append(names, id)
	}
	sort.Strings(names)
	result := []observability.Stored{}
	size := 0
	for _, id := range names {
		body := j.files[id]
		if len(result) == count || len(body) > limit-size {
			break
		}
		result = append(result, observability.Stored{ID: id, Body: append([]byte(nil), body...)})
		size += len(body)
	}
	return result, ctx.Err()
}
func (j *journal) Remove(ctx context.Context, id string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failRemove {
		return errors.New("remove failed")
	}
	delete(j.files, id)
	return ctx.Err()
}
func (j *journal) Flush(ctx context.Context) error {
	if j.flush != nil {
		return j.flush(ctx)
	}
	return ctx.Err()
}

type logStore struct {
	rows            map[string][]byte
	down, lostReply bool
	calls           int
}

func (s *logStore) StoreBatch(_ context.Context, b []observability.SafeEvent) error {
	s.calls++
	if s.down {
		return errors.New("logs down")
	}
	if s.rows == nil {
		s.rows = map[string][]byte{}
	}
	for _, e := range b {
		if old, ok := s.rows[e.ID()]; ok && string(old) != string(e.Bytes()) {
			return observability.ErrConflict
		}
		s.rows[e.ID()] = e.Bytes()
	}
	if s.lostReply {
		s.lostReply = false
		return errors.New("commit reply lost")
	}
	return nil
}
func buffer(t *testing.T, j observability.Journal) *observability.Buffer {
	t.Helper()
	b, err := observability.NewBuffer(quota(t, j), sanitizer(t), 10, 32768)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func appendEvent(t *testing.T, b *observability.Buffer, n int) observability.SafeEvent {
	t.Helper()
	e := event()
	e.ID = fmt.Sprintf("event-%d", n)
	safe, err := sanitizer(t).Sanitize(e, observability.Details{Message: "fictional-secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(context.Background(), safe); err != nil {
		t.Fatal(err)
	}
	return safe
}
func TestT05_08UnavailableLogsRetainFilesAcrossComponentRebuild(t *testing.T) {
	j := newJournal()
	b := buffer(t, j)
	appendEvent(t, b, 1)
	store := &logStore{down: true}
	if _, err := b.Deliver(context.Background(), store); err != observability.ErrDelivery || len(j.files) != 1 {
		t.Fatal("unacknowledged event removed")
	}
	rebuilt := buffer(t, j)
	store.down = false
	if n, err := rebuilt.Deliver(context.Background(), store); err != nil || n != 1 || len(j.files) != 0 || len(store.rows) != 1 {
		t.Fatal("restart failed to replay durable event")
	}
}
func TestT05_09LostCommitReplyReplaysSameIdentityBeforeReclaim(t *testing.T) {
	j := newJournal()
	b := buffer(t, j)
	appendEvent(t, b, 1)
	store := &logStore{lostReply: true}
	if _, err := b.Deliver(context.Background(), store); err != observability.ErrDelivery || len(j.files) != 1 || len(store.rows) != 1 {
		t.Fatal("lost reply treated as acknowledged")
	}
	if n, err := buffer(t, j).Deliver(context.Background(), store); err != nil || n != 1 || len(j.files) != 0 || len(store.rows) != 1 || store.calls != 2 {
		t.Fatal("replay was not idempotent")
	}
}
func TestT05_05OnlySanitizedBytesReachDurableFiles(t *testing.T) {
	j := newJournal()
	b := buffer(t, j)
	appendEvent(t, b, 1)
	for _, body := range j.files {
		if strings.Contains(string(body), "fictional-secret-token") {
			t.Fatal("secret in durable buffer")
		}
	}
	if b.Append(context.Background(), observability.SafeEvent{}) == nil {
		t.Fatal("zero safe event entered persistence")
	}
	for id, body := range j.files {
		j.files[id] = []byte(strings.Replace(string(body), id, "Student-00019", 1))
	}
	if _, err := buffer(t, j).Pending(context.Background()); err == nil {
		t.Fatal("corrupted identity restored")
	}
}
func TestDirectoryDurableReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	files, err := observability.OpenDirectory(path, testkit.FixedRandom(4), 4096)
	if err != nil {
		t.Fatal(err)
	}
	b := buffer(t, files)
	safe := appendEvent(t, b, 1)
	if err := b.Append(ctx, safe); err != nil {
		t.Fatal(err)
	}
	if err := files.Put(ctx, safe.ID(), []byte(`{"different":true}`)); err != observability.ErrConflict {
		t.Fatal("same key overwritten")
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	files, err = observability.OpenDirectory(path, testkit.FixedRandom(5), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if n, err := buffer(t, files).Deliver(ctx, &logStore{}); err != nil || n != 1 {
		t.Fatal("real directory reopen lost record")
	}
	pending, err := buffer(t, files).Pending(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatal("acknowledged file not reclaimed")
	}
}
func TestDirectoryBatchBoundDoesNotPreventDrainingBacklog(t *testing.T) {
	ctx := context.Background()
	d, err := observability.OpenDirectory(t.TempDir(), testkit.FixedRandom(6), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b := buffer(t, d)
	for i := 0; i < 25; i++ {
		appendEvent(t, b, i)
	}
	total := 0
	store := &logStore{}
	for i := 0; i < 3; i++ {
		n, err := b.Deliver(ctx, store)
		if err != nil || n > 10 {
			t.Fatal("backlog could not make bounded progress")
		}
		total += n
	}
	if total != 25 || len(store.rows) != 25 {
		t.Fatal("backlog was truncated")
	}
	if strings.Contains(fmt.Sprintf("%#v %#v", d, *d), "maxEventBytes") {
		t.Fatal("directory diagnostic bypass")
	}
}
