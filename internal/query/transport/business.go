package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
)

type UseCase func(context.Context, any) (any, error)

// Observation receives a span whose frozen event could not be acknowledged.
// The owner must retain/retry or report it; the business outcome is unchanged.
type Observation func(*events.Span, error)

// Business wraps real auth/parse/use-case/response behavior. Request metadata
// and operation are server supplied; no request fields are copied into logs.
func Business(op contract.Operation, key string, recorder *events.Recorder, observe Observation, run UseCase) (http.Handler, error) {
	return business(op, key, recorder, observe, run, false)
}

func BusinessWithSnapshot(op contract.Operation, key string, recorder *events.Recorder, observe Observation, run UseCase) (http.Handler, error) {
	return business(op, key, recorder, observe, run, true)
}

func business(op contract.Operation, key string, recorder *events.Recorder, observe Observation, run UseCase, snapshots bool) (http.Handler, error) {
	if recorder == nil || observe == nil || run == nil {
		return nil, errors.New("query dependencies required")
	}
	known := false
	for _, d := range contract.Definitions() {
		if d.Operation == op {
			known = true
		}
	}
	if !known {
		return nil, errors.New("unknown query operation")
	}
	guarded, err := APIKey(key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params, err := Parse(op, r.Body)
		if err != nil {
			WriteFailure(w, r, err)
			return
		}
		token, err := requestSnapshot(r.Header, op, snapshots)
		if err != nil {
			WriteFailure(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), snapshotKey{}, token)
		data, err := run(ctx, params)
		if err != nil {
			var failure *Failure
			if !errors.As(err, &failure) {
				category := contract.DependencyFailure
				switch {
				case errors.Is(err, read.ErrBudget), errors.Is(err, cache.ErrCapacity):
					category = contract.ResourceExhausted
				case errors.Is(err, snapshot.ErrSnapshot):
					category = contract.Conflict
				case errors.Is(err, read.ErrHandle):
					category = contract.NotReady
				case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
					category = contract.DeadlineExceeded
				}
				err = &Failure{Category: category, Cause: err}
			}
			WriteFailure(w, r, err)
			return
		}
		if reply, ok := data.(Reply); ok {
			if reply.Snapshot != "" {
				if !snapshots || op != contract.StudentGrades && op != contract.BatchGrades || !validSnapshot(reply.Snapshot) {
					WriteFailure(w, r, &Failure{Category: contract.Internal})
					return
				}
				w.Header().Set(snapshot.Header, reply.Snapshot)
			}
			data = reply.Data
		}
		WriteSuccess(w, r, data)
	}))
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := events.WithMetadata(r.Context(), events.Metadata{Operation: op})
		ctx, span, err := recorder.Begin(ctx, events.Request, events.Identity{RequestID: RequestID(ctx)}, "", 0)
		if err != nil {
			WriteFailure(w, r, &Failure{Category: contract.DependencyFailure})
			return
		}
		if RequestID(ctx) == "" {
			ctx = WithRequestID(ctx, span.Identity().RequestID)
		}
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", RequestID(ctx))
		buffer := &responseBuffer{header: make(http.Header)}
		guarded.ServeHTTP(buffer, r)
		if buffer.err != nil {
			buffer = &responseBuffer{header: make(http.Header)}
			WriteFailure(buffer, r, &Failure{Category: contract.ResourceExhausted})
		}
		status := buffer.status
		if status == 0 {
			status = 200
		}
		outcome := events.Succeeded
		if status >= 400 {
			outcome = events.Failed
			buffer.header.Del(snapshot.Header)
		}
		if err := span.Finish(ctx, outcome, &status, nil); err != nil {
			observe(span, err)
		}
		for k, values := range buffer.header {
			w.Header()[k] = append([]string(nil), values...)
		}
		w.WriteHeader(status)
		n, writeErr := w.Write(buffer.body.Bytes())
		delivery := events.Succeeded
		if writeErr != nil || n != buffer.body.Len() || ctx.Err() != nil {
			delivery = events.Interrupted
		}
		if err := span.Transport(ctx, delivery, &status); err != nil {
			observe(span, err)
		}
	}), nil
}

// Responses are committed only after complete business encoding. Cap this
// buffer independently of source scan bytes (serialization may expand strings).
type responseBuffer struct {
	header http.Header
	status int
	body   bytes.Buffer
	err    error
}

func (b *responseBuffer) Header() http.Header { return b.header }
func (b *responseBuffer) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}
func (b *responseBuffer) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if len(p) > 16<<20-b.body.Len() {
		b.err = io.ErrShortBuffer
		return 0, b.err
	}
	if b.status == 0 {
		b.status = 200
	}
	return b.body.Write(p)
}
