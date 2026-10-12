package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type cutoverDB struct {
	closed bool
	calls  int
}

func (d *cutoverDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	d.calls++
	return nil, errors.New("unexpected business work")
}
func (d *cutoverDB) Close(context.Context) error { d.closed = true; return nil }

type cutoverRedis struct {
	closed bool
	calls  int
}

func (r *cutoverRedis) Do(context.Context, string, ...any) (any, error) {
	r.calls++
	return nil, errors.New("unexpected business work")
}
func (r *cutoverRedis) Close(context.Context) error { r.closed = true; return nil }

func TestT28_13CutoverOneShotDoesNotStartBusinessAndReleasesOwnership(t *testing.T) {
	v := testkit.RuntimeValues(t.TempDir())
	c, err := config.LoadRuntime(config.Query, func(k string) (string, bool) { value, ok := v[k]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	if os.Mkdir(c.LogDirectory, 0700) != nil {
		t.Fatal("mkdir")
	}
	db, redis := &cutoverDB{}, &cutoverRedis{}
	f := assembly.RealFactories()
	f.Postgres = func(context.Context, config.Runtime) (assembly.Database, error) { return db, nil }
	f.Redis = func(context.Context, config.Runtime) (assembly.Redis, error) { return redis, nil }
	if err = prepareLogCutover(t.Context(), c, f); err != nil {
		t.Fatal(err)
	}
	if !db.closed || !redis.closed || db.calls != 0 || redis.calls != 0 {
		t.Fatal("business work started or resource leaked")
	}
	j, err := observability.OpenDirectory(c.LogDirectory, testkit.FixedRandom(3), 16384)
	if err != nil {
		t.Fatal("journal ownership not released")
	}
	defer j.Close()
	if pin, err := j.LegacyTarget(t.Context()); err != nil || pin == "" {
		t.Fatal("original target not pinned")
	}
}
