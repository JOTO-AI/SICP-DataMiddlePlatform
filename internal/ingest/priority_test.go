package ingest

import "testing"

func TestT32_11LowPriorityUsesSamePermitsAndCannotQueueAheadOfSynchronization(t *testing.T) {
	l, e := NewLimiter(2)
	if e != nil {
		t.Fatal(e)
	}
	release, e := l.Acquire(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = l.LowPriority().Acquire(t.Context()); e == nil {
		t.Fatal("test consumed free permit while synchronization active")
	}
	release()
	release()
	l.mu.Lock()
	l.waiting = 1
	l.mu.Unlock()
	if _, e = l.LowPriority().Acquire(t.Context()); e == nil {
		t.Fatal("test jumped queued synchronization")
	}
	l.mu.Lock()
	l.waiting = 0
	l.mu.Unlock()
	release, e = l.LowPriority().Acquire(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if len(l.permits) != 1 {
		t.Fatal("test used a separate limiter")
	}
	release()
	release()
}
