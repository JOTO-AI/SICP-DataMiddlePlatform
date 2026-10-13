// Package streamio bounds writes and interrupts a blocked stream on cancellation.
package streamio

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Writer serializes deadline changes with cancellation, so a concurrent write
// cannot extend a deadline after logout. Only the handler calls Write/Flush.
type Writer struct {
	ctx     context.Context
	w       http.ResponseWriter
	control *http.ResponseController
	timeout time.Duration
	mu      sync.Mutex
	stop    func() bool
	done    chan struct{}
}

func New(ctx context.Context, w http.ResponseWriter, timeout time.Duration) *Writer {
	s := &Writer{ctx: ctx, w: w, control: http.NewResponseController(w), timeout: timeout, done: make(chan struct{})}
	s.stop = context.AfterFunc(ctx, func() {
		s.mu.Lock()
		_ = s.control.SetWriteDeadline(time.Now())
		s.mu.Unlock()
		close(s.done)
	})
	return s
}

// Close waits for the cancellation callback before the HTTP handler returns.
func (s *Writer) Close() {
	if !s.stop() {
		<-s.done
	}
}

func (s *Writer) Prepare() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(s.timeout)
	if end, ok := s.ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	return s.control.SetWriteDeadline(deadline)
}

func (s *Writer) Write(p []byte) (int, error) {
	if err := s.Prepare(); err != nil {
		return 0, err
	}
	return s.w.Write(p)
}

func (s *Writer) Flush() error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.control.Flush()
}
