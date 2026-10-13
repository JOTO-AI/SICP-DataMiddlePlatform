package logquery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type scanEntry struct {
	done  chan struct{}
	at    time.Time
	batch Batch
	err   error
}
type StreamServer struct {
	Service     *Service
	connections chan struct{}
	mu          sync.Mutex
	cache       map[string]*scanEntry
	sources     map[Source]chan struct{}
}

func NewStreamServer(s *Service) *StreamServer {
	return &StreamServer{Service: s, connections: make(chan struct{}, 16), cache: map[string]*scanEntry{}, sources: map[Source]chan struct{}{Platform: make(chan struct{}, 2), Upstream: make(chan struct{}, 2)}}
}

// Scans are shared by durable position, irrespective of filter/actor. Only raw
// validated records are reused; each subscriber separately authorizes/filters.
func (s *StreamServer) scan(ctx context.Context, c Cursor) (Batch, error) {
	key := string(c.Source) + "/" + c.Epoch + "/" + strconv.FormatInt(c.Position, 10)
	s.mu.Lock()
	now := time.Now()
	for k, e := range s.cache {
		select {
		case <-e.done:
			if now.Sub(e.at) >= time.Second {
				delete(s.cache, k)
			}
		default:
		}
	}
	entry := s.cache[key]
	if entry != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Batch{}, readError(ctx)
		case <-entry.done:
			if entry.err != nil {
				return Batch{}, entry.err
			}
			if err := c.ValidateContinuation(c.Filter, entry.batch.Snapshot.Epoch, entry.batch.Snapshot.Floor); err != nil {
				return Batch{}, err
			}
			if c.HighWater > entry.batch.Snapshot.HighWater {
				return Batch{}, ErrExpired
			}
			return entry.batch, nil
		}
	}
	if len(s.cache) >= 16 {
		s.mu.Unlock()
		return Batch{}, ErrLimit
	}
	select {
	case s.sources[c.Source] <- struct{}{}:
	default:
		s.mu.Unlock()
		return Batch{}, ErrLimit
	}
	entry = &scanEntry{done: make(chan struct{}), at: now}
	s.cache[key] = entry
	s.mu.Unlock()
	entry.batch, entry.err = s.Service.read(ctx, c.Source, ReadRequest{Filter: c.Filter, Cursor: &c, Stream: true})
	s.mu.Lock()
	entry.at = time.Now()
	close(entry.done)
	if entry.err != nil {
		delete(s.cache, key)
	}
	s.mu.Unlock()
	<-s.sources[c.Source]
	return entry.batch, entry.err
}
func matches(f Filter, r Record) bool {
	e := r.Event.Event()
	i := e.Identity
	return !e.OccurredAt.Before(f.From) && (f.Kind == "" || f.Kind == e.Kind) && (f.Phase == "" || f.Phase == e.Phase) && (f.Outcome == "" || f.Outcome == e.Outcome) && (f.Operation == "" || f.Operation == e.Metadata.Operation) && (f.TaskID == "" || f.TaskID == i.TaskID) && (f.RequestID == "" || f.RequestID == i.RequestID) && (f.TestRunID == "" || f.TestRunID == i.TestRunID)
}
func (s *StreamServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.Service == nil {
		streamHTTPError(w, ErrUnavailable)
		return
	}
	source := Source(r.PathValue("source"))
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !source.Valid() {
		streamHTTPError(w, ErrArgument)
		return
	}
	if len(r.Header.Values("Last-Event-ID")) > 1 {
		streamHTTPError(w, ErrCursor)
		return
	}
	token := values.Get("cursor")
	if header := r.Header.Get("Last-Event-ID"); header != "" {
		token = header
	}
	c, err := s.Service.Signer.Decode(token, source, Stream, r.Header.Get("X-SICP-Actor"), s.Service.Now())
	if err != nil {
		streamHTTPError(w, err)
		return
	}
	// Preserve duplicate-parameter rejection even when the header takes priority.
	if len(values["cursor"]) > 1 {
		streamHTTPError(w, ErrArgument)
		return
	}
	values.Set("cursor", token)
	if _, _, err = ParseFilter(values, s.Service.Now(), &c.Filter, s.Service.Correlator); err != nil {
		streamHTTPError(w, err)
		return
	}
	select {
	case s.connections <- struct{}{}:
		defer func() { <-s.connections }()
	default:
		streamHTTPError(w, ErrLimit)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	b, err := s.scan(ctx, c)
	if err != nil {
		streamHTTPError(w, err)
		return
	}
	controller := http.NewResponseController(w)
	if controller.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil {
		streamHTTPError(w, ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(kind, id string, value any) error {
		data, e := json.Marshal(value)
		if e != nil || len(data) > MaxEventBytes {
			return ErrLimit
		}
		if e = controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); e != nil {
			return e
		}
		if id != "" {
			if _, e = fmt.Fprintf(w, "id: %s\n", id); e != nil {
				return e
			}
		}
		if _, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data); e != nil {
			return e
		}
		return controller.Flush()
	}
	control := func(err error) {
		_, code := HTTPError(err)
		_ = write("control", "", map[string]any{"code": code, "retryable": err == ErrUnavailable || err == ErrTimeout || err == ErrLimit, "rebuild_required": err == ErrExpired || err == ErrCursor})
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		if !s.Service.Now().Before(c.ExpiresAt) {
			control(ErrExpired)
			return
		}
		for _, rec := range b.Records {
			if !s.Service.Now().Before(c.ExpiresAt) {
				control(ErrExpired)
				return
			}
			if rec.Sequence <= c.Position {
				control(ErrUnavailable)
				return
			}
			next := c
			next.Position = rec.Sequence
			if matches(c.Filter, rec) {
				token, e := s.Service.Signer.Encode(next)
				if e != nil {
					control(e)
					return
				}
				if write("event", token, project(c.Source, c.Epoch, rec)) != nil {
					return
				}
			}
			c = next
		}
		// A checkpoint is emitted only after every matching predecessor was sent.
		token, e := s.Service.Signer.Encode(c)
		if e != nil {
			control(e)
			return
		}
		if write("checkpoint", token, map[string]string{"sequence": strconv.FormatInt(c.Position, 10)}) != nil {
			return
		}
		select {
		case <-ctx.Done():
			if r.Context().Err() == nil {
				_ = write("control", "", map[string]any{"code": "connection_expired", "retryable": true, "rebuild_required": false})
			}
			return
		case <-heartbeat.C:
			if controller.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil {
				return
			}
			if _, e = fmt.Fprint(w, ": heartbeat\n\n"); e != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
		case <-poll.C:
		}
		b, err = s.scan(ctx, c)
		if err != nil {
			control(err)
			return
		}
	}
}
func streamHTTPError(w http.ResponseWriter, err error) {
	status, code := HTTPError(err)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if status == 429 {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": "Log stream unavailable", "data": nil})
}
