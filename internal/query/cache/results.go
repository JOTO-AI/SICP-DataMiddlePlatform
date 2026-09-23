package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
)

var ErrPayload = errors.New("invalid query cache payload")

const MaxPayloadBytes = 16 << 20

// Results stores successful business JSON only. Request envelopes and current
// synchronization metadata are composed outside this component.
type Results struct {
	Store   ports.Cache
	Observe func(error)
}
type Result struct {
	Data []byte
	Hit  bool
}

func validPayload(data []byte) bool {
	if len(data) == 0 || len(data) > MaxPayloadBytes {
		return false
	}
	value := bytes.TrimSpace(data)
	return len(value) > 0 && (value[0] == '{' || value[0] == '[') && json.Valid(value)
}

func (r Results) Lookup(ctx context.Context, key string) (Result, error) {
	if r.Store == nil || r.Observe == nil || key == "" {
		return Result{}, ErrKey
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	data, err := r.Store.Get(ctx, key)
	if err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	if data == nil {
		return Result{}, nil
	}
	if !validPayload(data) {
		return Result{}, ErrPayload
	}
	return Result{Data: bytes.Clone(data), Hit: true}, nil
}

func (r Results) Fill(ctx context.Context, key string, data []byte) error {
	if r.Store == nil || r.Observe == nil || key == "" {
		return ErrKey
	}
	if !validPayload(data) {
		return ErrPayload
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Store.Set(ctx, key, bytes.Clone(data))
}

// GetOrLoad does not silently convert a cache outage into a miss. The P11.06
// pipeline must separately establish permission and budgets for degradation.
func (r Results) GetOrLoad(ctx context.Context, key string, load func(context.Context) ([]byte, error)) (Result, error) {
	if load == nil {
		return Result{}, ErrPayload
	}
	cached, err := r.Lookup(ctx, key)
	if err != nil || cached.Hit {
		return cached, err
	}
	data, err := load(ctx)
	if err != nil {
		return Result{}, err
	}
	if !validPayload(data) {
		return Result{}, ErrPayload
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	result := Result{Data: bytes.Clone(data)}
	if err = r.Fill(ctx, key, data); err != nil {
		r.Observe(err)
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}
