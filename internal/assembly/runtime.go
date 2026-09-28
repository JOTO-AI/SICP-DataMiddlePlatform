// Package assembly constructs real dependencies only after validating the entire
// configuration. Factories are explicit test seams, never fallback providers.
package assembly

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/lifecycle"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type Database interface {
	storage.Database
	ports.Resource
}
type Redis interface {
	publication.Commander
	ports.Resource
}
type Source interface {
	Fetch(context.Context, ingest.PageRequest) (ingest.Page, error)
	ports.Resource
}
type Mail interface {
	notification.Channel
	ports.Resource
}
type FileSet struct {
	WorkerExclusive  bool
	Journal          observability.Journal
	Staging, Targets *staging.QuotaFiles
	close            func(context.Context) error
}

func (f *FileSet) Close(ctx context.Context) error {
	if f.close == nil {
		return errors.New("file cleanup unavailable")
	}
	return f.close(ctx)
}

type Factories struct {
	Postgres func(context.Context, config.Runtime) (Database, error)
	Redis    func(context.Context, config.Runtime) (Redis, error)
	Files    func(context.Context, config.Runtime) (*FileSet, error)
	Source   func(context.Context, config.Runtime) (Source, error)
	Mail     func(context.Context, config.Runtime) (Mail, error)
}
type Runtime struct {
	ProcessID string
	Database  Database
	Redis     Redis
	Files     *FileSet
	Source    Source
	Mail      Mail
	Control   control.Repository
	Active    publication.Atomic
	Cache     cache.Redis
	Sanitizer *observability.Sanitizer
	Logs      *observability.Buffer
	manager   *lifecycle.Manager
	once      sync.Once
	closed    error
}

func RealFactories() Factories {
	return Factories{
		Postgres: func(ctx context.Context, c config.Runtime) (Database, error) {
			p, e := adapters.OpenPostgres(ctx, c.Databases, c.DependencyTimeout, c.ProcessID)
			if p == nil {
				return nil, e
			}
			return p, e
		},
		Redis: func(ctx context.Context, c config.Runtime) (Redis, error) {
			r, e := adapters.OpenRedis(ctx, c.RedisURL, c.DependencyTimeout)
			if r == nil {
				return nil, e
			}
			return r, e
		},
		Files: openFiles,
		Source: func(_ context.Context, c config.Runtime) (Source, error) {
			s, e := adapters.OpenSource(c.SourceURL, c.SourceToken, c.SourceAppID, c.SourceAppSecret, c.DependencyTimeout)
			if s == nil {
				return nil, e
			}
			return s, e
		},
		Mail: func(_ context.Context, c config.Runtime) (Mail, error) {
			m, e := adapters.NewMail(c)
			if m == nil {
				return nil, e
			}
			return m, e
		},
	}
}

func Open(ctx context.Context, c config.Runtime, f Factories) (*Runtime, []lifecycle.Result, error) {
	if e := c.Validate(); e != nil {
		return nil, nil, e
	}
	if f.Postgres == nil || f.Redis == nil || f.Files == nil || c.Service.Role == config.Worker && (f.Source == nil || c.MailEnabled() && f.Mail == nil) {
		return nil, nil, errors.New("required dependency factory missing")
	}
	r := &Runtime{}
	var random [16]byte
	if _, e := rand.Read(random[:]); e != nil {
		return nil, nil, errors.New("process identity unavailable")
	}
	r.ProcessID = hex.EncodeToString(random[:])
	c.ProcessID = r.ProcessID
	key := sha256.Sum256([]byte(c.AuditKey))
	secrets := []string{}
	for _, v := range []string{c.Service.APIKey, c.SourceToken, c.SourceAppID, c.SourceAppSecret, c.SnapshotKey, c.AuditKey, c.MailPassword} {
		if len(v) >= 8 && len(v) <= 4096 {
			secrets = append(secrets, v)
		}
	}
	var e error
	r.Sanitizer, e = observability.NewSanitizer(16384, 2048, key[:], secrets)
	if e != nil {
		return nil, nil, errors.New("invalid event configuration")
	}
	steps := []lifecycle.Factory{
		{Name: "postgres", Open: func(ctx context.Context) (ports.Resource, error) {
			v, e := f.Postgres(ctx, c)
			r.Database = v
			return v, e
		}},
		{Name: "redis", Open: func(ctx context.Context) (ports.Resource, error) { v, e := f.Redis(ctx, c); r.Redis = v; return v, e }},
		{Name: "files", Open: func(ctx context.Context) (ports.Resource, error) {
			v, e := f.Files(ctx, c)
			r.Files = v
			if v == nil {
				return nil, e
			}
			if e == nil && (v.close == nil || v.Journal == nil || c.Service.Role == config.Worker && (v.Staging == nil || v.Targets == nil)) {
				e = errors.New("persistent file components missing")
			}
			return v, e
		}},
	}
	if c.Service.Role == config.Worker {
		steps = append(steps,
			lifecycle.Factory{Name: "source", Open: func(ctx context.Context) (ports.Resource, error) { v, e := f.Source(ctx, c); r.Source = v; return v, e }},
		)
		if c.MailEnabled() {
			steps = append(steps, lifecycle.Factory{Name: "mail", Open: func(ctx context.Context) (ports.Resource, error) { v, e := f.Mail(ctx, c); r.Mail = v; return v, e }})
		}
	}
	startup, cancel := context.WithTimeout(ctx, c.Service.StartupTimeout)
	defer cancel()
	m, results, e := lifecycle.Start(startup, steps, c.Service.ShutdownTimeout)
	if e != nil {
		return nil, results, e
	}
	r.manager = m
	// Remaining constructors are pure and checked. Failure still releases every
	// opened transport and directory, in reverse order, within one budget.
	fail := func() (*Runtime, []lifecycle.Result, error) {
		return nil, m.Close(c.Service.ShutdownTimeout), errors.New("runtime components initialization failed")
	}
	q, e := observability.NewQuota(r.Files.Journal, observability.CapacityConfig{MaxBytes: c.LogBytes, MaxFiles: c.LogFiles, WarningPercent: 80, Policy: observability.RejectNew})
	if e != nil {
		return fail()
	}
	r.Logs, e = observability.NewBuffer(q, r.Sanitizer, 100, 1<<20)
	if e != nil {
		return fail()
	}
	r.Control = control.Repository{Registry: c.Databases, Database: r.Database}
	r.Active = publication.Atomic{Client: r.Redis}
	r.Cache = cache.Redis{Client: r.Redis}
	return r, nil, nil
}
func (r *Runtime) Close(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("shutdown deadline required")
	}
	r.once.Do(func() {
		if r.manager == nil {
			return
		}
		for _, v := range r.manager.Close(time.Until(deadline)) {
			if v.State != "closed" {
				r.closed = errors.New("dependency shutdown incomplete")
			}
		}
	})
	return r.closed
}
