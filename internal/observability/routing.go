package observability

import (
	"context"
	"reflect"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func eventSource(kind events.Kind) storage.LogSource {
	switch kind {
	case events.Request, events.Task, events.Step, events.Audit:
		return storage.PlatformLog
	case events.Call, events.Attempt:
		return storage.UpstreamLog
	default:
		return ""
	}
}

func acceptsSource(source storage.LogSource, e SafeEvent) bool {
	kindSource := eventSource(e.Event().Kind)
	return kindSource != "" && (source == "" || source == kindSource)
}

// NewSourceBuffer binds replay as well as admission to a single source. Legacy
// journals must first be drained to their original target, never reclassified.
func NewSourceBuffer(source storage.LogSource, files *QuotaJournal, s *Sanitizer, count, bytes int) (*Buffer, error) {
	if source != storage.PlatformLog && source != storage.UpstreamLog {
		return nil, ErrBuffer
	}
	b, err := NewBuffer(files, s, count, bytes)
	if err != nil {
		return nil, err
	}
	b.source = source
	return b, nil
}

type Router struct{ platform, upstream *Buffer }

// Each buffer must own its source directory, quota and dispatcher. Routing has
// no shared admission lock and never waits on either database.
func NewRouter(platform, upstream *Buffer) (*Router, error) {
	if platform == nil || upstream == nil || platform.source != storage.PlatformLog || upstream.source != storage.UpstreamLog || platform.files == upstream.files {
		return nil, ErrBuffer
	}
	p, u := reflect.ValueOf(platform.files.files), reflect.ValueOf(upstream.files.files)
	if !p.IsValid() || !u.IsValid() || !p.Comparable() || !u.Comparable() || p.Equal(u) {
		return nil, ErrBuffer
	}
	return &Router{platform, upstream}, nil
}

func (r *Router) Append(ctx context.Context, e SafeEvent) error {
	if r == nil || !e.Valid() {
		return ErrPayload
	}
	switch eventSource(e.Event().Kind) {
	case storage.PlatformLog:
		return r.platform.Append(ctx, e)
	case storage.UpstreamLog:
		return r.upstream.Append(ctx, e)
	default:
		return ErrPayload
	}
}
