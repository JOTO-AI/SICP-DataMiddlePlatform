// Package testkit provides synthetic dependencies; it never reads source_samples.
package testkit

import (
	"context"
	"time"
)

type FakeClock struct{ Time time.Time }

func (c FakeClock) Now() time.Time { return c.Time }

type FixedRandom byte

func (r FixedRandom) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

type CloseFunc func(context.Context) error

func (f CloseFunc) Close(ctx context.Context) error { return f(ctx) }

type ReadyFunc func(context.Context) bool

func (f ReadyFunc) Ready(ctx context.Context) bool { return f(ctx) }
