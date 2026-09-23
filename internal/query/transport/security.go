package transport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

type requestIDKey struct{}

// WithRequestID accepts only server-generated IDs, never an incoming header.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type Failure struct {
	Category contract.ErrorCategory
	Cause    error
}

func (e *Failure) Error() string { return string(e.Category) }
func (e *Failure) Unwrap() error { return e.Cause }

type Envelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Data      any    `json:"data"`
}

// WriteFailure exposes only catalogued messages. Causes remain internal.
func WriteFailure(w http.ResponseWriter, r *http.Request, err error) {
	category := contract.Internal
	var failure *Failure
	if errors.As(err, &failure) && failure != nil {
		category = failure.Category
	} else if errors.Is(err, context.DeadlineExceeded) {
		category = contract.DeadlineExceeded
	}
	status, message := http.StatusInternalServerError, "Internal error"
	switch category {
	case contract.InvalidArgument:
		status, message = http.StatusBadRequest, "Invalid request"
	case contract.Unauthenticated:
		status, message = http.StatusUnauthorized, "Authentication required"
	case contract.Forbidden:
		status, message = http.StatusForbidden, "Access denied"
	case contract.NotReady:
		status, message = http.StatusServiceUnavailable, "Service not ready"
	case contract.Conflict:
		status, message = http.StatusConflict, "Snapshot conflict"
	case contract.DependencyFailure:
		status, message = http.StatusServiceUnavailable, "Dependency unavailable"
	case contract.DeadlineExceeded:
		status, message = http.StatusGatewayTimeout, "Query timed out"
	default:
		category = contract.Internal
	}
	writeEnvelope(w, status, Envelope{string(category), message, RequestID(r.Context()), nil})
}

func WriteSuccess(w http.ResponseWriter, r *http.Request, data any) {
	writeEnvelope(w, http.StatusOK, Envelope{"ok", "OK", RequestID(r.Context()), data})
}

func writeEnvelope(w http.ResponseWriter, status int, body Envelope) {
	// Marshal before writing headers, so an invalid result cannot become a
	// truncated HTTP 200. Application handlers must finish all reads first.
	payload, err := json.Marshal(body)
	if err != nil {
		status = http.StatusInternalServerError
		payload, _ = json.Marshal(Envelope{string(contract.Internal), "Internal error", body.RequestID, nil})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(payload, '\n'))
}

// APIKey guards a complete business pipeline, including all cache reads.
// This shared service credential supplies no end-user identity or admin rights.
func APIKey(key string, next http.Handler) (http.Handler, error) {
	if !validKey(key) || next == nil {
		return nil, fmt.Errorf("valid query API key and handler required")
	}
	expected := sha256.Sum256([]byte(key))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var values []string
		// Header maps normally canonicalize names; also reject duplicates from
		// trusted adapters which constructed a map directly.
		for name, v := range r.Header {
			if strings.EqualFold(name, "X-API-Key") {
				values = append(values, v...)
			}
		}
		if len(values) != 1 || !validKey(values[0]) {
			WriteFailure(w, r, &Failure{Category: contract.Unauthenticated})
			return
		}
		actual := sha256.Sum256([]byte(values[0]))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			WriteFailure(w, r, &Failure{Category: contract.Unauthenticated})
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}

func validKey(key string) bool {
	if len(key) == 0 || len(key) > 4096 {
		return false
	}
	for _, c := range key {
		if c < 33 || c > 126 || c == ',' {
			return false
		}
	}
	return true
}
