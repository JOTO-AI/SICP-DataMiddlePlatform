package cache

import (
	"bytes"
	"context"
	"errors"
)

// Commander is the Redis command boundary; the network client is wired in P15.
type Commander interface {
	Do(context.Context, string, ...any) (any, error)
}
type Redis struct{ Client Commander }

var ErrRedis = errors.New("invalid Redis cache response")

// GET nil is a cache miss; transport/protocol failures remain errors.
func (r Redis) Get(ctx context.Context, key string) ([]byte, error) {
	if r.Client == nil {
		return nil, ErrRedis
	}
	value, err := r.Client.Do(ctx, "GET", key)
	if err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []byte:
		return bytes.Clone(v), nil
	case string:
		return []byte(v), nil
	default:
		return nil, ErrRedis
	}
}

// SET has deliberately no EX/PX/TTL option. Success is its own acknowledgement,
// never a subsequent GET that could conceal a failed or ambiguous write.
func (r Redis) Set(ctx context.Context, key string, value []byte) error {
	if r.Client == nil {
		return ErrRedis
	}
	response, err := r.Client.Do(ctx, "SET", key, bytes.Clone(value))
	if err != nil {
		return err
	}
	if text, ok := response.(string); ok && text == "OK" {
		return nil
	}
	if data, ok := response.([]byte); ok && string(data) == "OK" {
		return nil
	}
	return ErrRedis
}
