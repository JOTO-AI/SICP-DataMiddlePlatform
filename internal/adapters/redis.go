package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrRedis = errors.New("Redis operation failed")

type Redis struct{ client *redis.Client }

func (*Redis) String() string     { return "[Redis adapter redacted]" }
func (r *Redis) GoString() string { return r.String() }
func OpenRedis(ctx context.Context, raw string, timeout time.Duration) (*Redis, error) {
	o, e := redis.ParseURL(raw)
	if e != nil {
		return nil, ErrRedis
	}
	o.Protocol = 2
	o.MaxRetries = -1
	o.DialTimeout = timeout
	o.ReadTimeout = timeout
	o.WriteTimeout = timeout
	o.ContextTimeoutEnabled = true
	r := &Redis{redis.NewClient(o)}
	if e = r.client.Ping(ctx).Err(); e != nil {
		return r, ErrRedis
	}
	return r, nil
}
func (r *Redis) Do(ctx context.Context, command string, args ...any) (any, error) {
	v, e := r.client.Do(ctx, append([]any{command}, args...)...).Result()
	if errors.Is(e, redis.Nil) {
		return nil, nil
	}
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrRedis
	}
	return v, nil
}
func (r *Redis) Close(ctx context.Context) error {
	if e := r.client.Close(); e != nil {
		return ErrRedis
	}
	return ctx.Err()
}
