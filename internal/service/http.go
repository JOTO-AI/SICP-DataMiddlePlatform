package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

type Metadata struct {
	RequestID string
	StartedAt time.Time
}
type metadataKey struct{}

func RequestMetadata(ctx context.Context) Metadata {
	m, _ := ctx.Value(metadataKey{}).(Metadata)
	return m
}

type HTTP struct {
	handler  http.Handler
	draining atomic.Bool
}

func (h *HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.handler.ServeHTTP(w, r) }
func (h *HTTP) Stop()                                            { h.draining.Store(true) }

func NewHTTP(role config.Role, apiKey string, clock ports.Clock, random ports.Random, readiness ports.Readiness) (*HTTP, error) {
	return NewHTTPWithHandlers(role, apiKey, clock, random, readiness, nil)
}

// NewHTTPWithHandlers installs only implemented, authenticated query handlers.
// Missing operations retain guarded placeholders and keep readiness false.
func NewHTTPWithHandlers(role config.Role, apiKey string, clock ports.Clock, random ports.Random, readiness ports.Readiness, handlers map[contract.Operation]http.Handler) (*HTTP, error) {
	return newHTTP(role, apiKey, clock, random, readiness, handlers, nil)
}
func newHTTP(role config.Role, apiKey string, clock ports.Clock, random ports.Random, readiness ports.Readiness, handlers map[contract.Operation]http.Handler, management http.Handler) (*HTTP, error) {
	if clock == nil || random == nil {
		return nil, fmt.Errorf("clock and random dependencies required")
	}
	if role != config.Query && role != config.Worker {
		return nil, fmt.Errorf("invalid service role")
	}
	for op, handler := range handlers {
		known := false
		for _, d := range contract.Definitions() {
			if d.Operation == op {
				known = true
			}
		}
		if role != config.Query || !known || handler == nil {
			return nil, fmt.Errorf("invalid query handler registration")
		}
	}
	h := &HTTP{}
	mux := http.NewServeMux()
	registry := transport.NewRegistry()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"live": true})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		fields := map[string]bool{}
		dependenciesReady := false
		if health, ok := readiness.(interface {
			Status(context.Context) map[string]bool
		}); ok {
			for k, v := range health.Status(r.Context()) {
				fields[k] = v && !h.draining.Load()
			}
			if role == config.Query {
				dependenciesReady = fields["query_ready"]
			} else {
				dependenciesReady = fields["sync_ready"]
			}
		} else {
			dependenciesReady = !h.draining.Load() && readiness != nil && readiness.Ready(r.Context())
		}
		// A valid data combination alone cannot make placeholder handlers ready.
		ready := dependenciesReady && (role == config.Query && registry.Ready() || role == config.Worker && management != nil)
		status := http.StatusServiceUnavailable
		if ready {
			status = http.StatusOK
		}
		fields["ready"] = ready
		writeJSON(w, status, fields)
	})
	if role == config.Worker && management != nil {
		mux.Handle("/admin/", management)
		mux.Handle("/console-api/", management)
	}
	if role == config.Query {
		if management != nil {
			mux.Handle("/internal/admin/", management)
		}
		// Validate the configured credential even when all handlers are supplied.
		if _, err := transport.APIKey(apiKey, http.NotFoundHandler()); err != nil {
			return nil, err
		}
		for _, definition := range contract.Definitions() {
			if handler := handlers[definition.Operation]; handler != nil {
				if err := registry.Enable(definition, handler); err != nil {
					return nil, err
				}
				continue
			}
			placeholder, err := transport.APIKey(apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				transport.WriteFailure(w, r, &transport.Failure{Category: contract.NotReady})
			}))
			if err != nil {
				return nil, err
			}
			if err := registry.Register(definition, placeholder); err != nil {
				return nil, err
			}
		}
		mux.Handle("/api/", registry.Handler())
	}
	// Serialize injected readers as they need not be concurrency safe.
	var randomMu sync.Mutex
	h.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var id [16]byte
		randomMu.Lock()
		_, err := io.ReadFull(random, id[:])
		randomMu.Unlock()
		if err != nil {
			transport.WriteFailure(w, r, &transport.Failure{Category: contract.Internal})
			return
		}
		m := Metadata{hex.EncodeToString(id[:]), clock.Now()}
		w.Header().Set("X-Request-ID", m.RequestID)
		ctx := transport.WithRequestID(context.WithValue(r.Context(), metadataKey{}, m), m.RequestID)
		r = r.WithContext(ctx)
		if h.draining.Load() && r.URL.Path != "/livez" && r.URL.Path != "/readyz" {
			transport.WriteFailure(w, r, &transport.Failure{Category: contract.NotReady})
			return
		}
		mux.ServeHTTP(w, r)
	})
	return h, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
