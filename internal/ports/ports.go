// Package ports contains the dependency boundaries shared by both services.
package ports

import (
	"context"
	"io"
	"net/http"
	"time"
)

type Clock interface{ Now() time.Time }
type Random interface{ Read([]byte) (int, error) }
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// FileStore is the staging/file boundary, not a business database repository.
// Implementations must respect cancellation and keep paths within their root.
type FileStore interface {
	Open(context.Context, string) (io.ReadCloser, error)
	Create(context.Context, string) (io.WriteCloser, error)
	Remove(context.Context, string) error
}

type Cache interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte) error
}

type Notification struct{ ID, Subject, Body string }
type Notifier interface {
	Notify(context.Context, Notification) error
}

// Resource owns a dependency's bounded shutdown. A nil error means closed.
type Resource interface{ Close(context.Context) error }

// Readiness is supplied by the combination/coordinator implementation in P11.
// It must be concurrency safe and honor the request context.
type Readiness interface{ Ready(context.Context) bool }

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
