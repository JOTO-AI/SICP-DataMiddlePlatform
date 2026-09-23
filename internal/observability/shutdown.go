package observability

import "context"

// Close implements ports.Resource. The caller's single lifecycle budget covers
// admission, Run, timer cleanup, file flush and all final delivery attempts.
// A dependency ignoring cancellation may outlive it; that is reported incomplete.
func (d *Dispatcher) Close(ctx context.Context) error {
	d.buffer.closed.Store(true)
	d.runMu.Lock()
	started := d.runStarted
	if d.runCancel != nil {
		d.runCancel()
	}
	d.runMu.Unlock()
	d.closeOnce.Do(func() { go func() { d.closeErr = d.drain(ctx, started); close(d.closeDone) }() })
	select {
	case <-ctx.Done():
		return ErrIncomplete
	case <-d.closeDone:
		return d.closeErr
	}
}
func (d *Dispatcher) drain(ctx context.Context, started bool) error {
	if started {
		select {
		case <-d.runDone:
		case <-ctx.Done():
			return ErrIncomplete
		}
	}
	waitersDone := make(chan struct{})
	go func() { d.waiters.Wait(); close(waitersDone) }()
	select {
	case <-waitersDone:
	case <-ctx.Done():
		return ErrIncomplete
	}
	select {
	case d.buffer.appendGate <- struct{}{}:
		<-d.buffer.appendGate
	case <-ctx.Done():
		return ErrIncomplete
	}
	select {
	case d.gate <- struct{}{}:
		<-d.gate
	case <-ctx.Done():
		return ErrIncomplete
	}
	if err := d.buffer.files.Flush(ctx); err != nil || ctx.Err() != nil {
		return ErrIncomplete
	}
	for {
		if ctx.Err() != nil {
			return ErrIncomplete
		}
		count, err := d.buffer.Deliver(ctx, d.store)
		d.delivered.Add(uint64(count))
		if err != nil {
			return ErrIncomplete
		}
		if count == 0 {
			return nil
		}
	}
}
