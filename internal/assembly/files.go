package assembly

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

func openFiles(ctx context.Context, c config.Runtime) (*FileSet, error) {
	var closers []func() error
	f := &FileSet{close: func(ctx context.Context) error {
		var combined error
		for i := len(closers) - 1; i >= 0; i-- {
			combined = errors.Join(combined, closers[i]())
		}
		return errors.Join(combined, ctx.Err())
	}}
	if ctx.Err() != nil {
		return f, ctx.Err()
	}
	paths := []string{c.LogDirectory}
	if c.Service.Role == config.Worker {
		paths = append(paths, c.StagingDirectory, c.TargetDirectory, c.SpoolDirectory)
	}
	var resolved []string
	for _, p := range paths {
		canonical, e := filepath.EvalSymlinks(p)
		if e != nil {
			return f, errors.New("persistent directory unavailable")
		}
		for _, old := range resolved {
			if canonical == old || strings.HasPrefix(canonical, old+string(os.PathSeparator)) || strings.HasPrefix(old, canonical+string(os.PathSeparator)) {
				return f, errors.New("persistent directory overlap")
			}
		}
		resolved = append(resolved, canonical)
	}
	if c.Service.Role == config.Worker {
		unlock, e := lockWorker(c.SpoolDirectory)
		if e != nil {
			return f, e
		}
		closers = append(closers, unlock)
		f.WorkerExclusive = true
	}
	for _, p := range paths {
		probe, e := os.CreateTemp(p, ".probe-")
		if e != nil {
			return f, errors.New("persistent directory not writable")
		}
		name := probe.Name()
		e = probe.Close()
		removeErr := os.Remove(name)
		if e != nil || removeErr != nil {
			return f, errors.New("persistent directory not writable")
		}
	}
	j, e := observability.OpenDirectory(c.LogDirectory, rand.Reader, 16384)
	if e != nil {
		return f, e
	}
	closers = append(closers, j.Close)
	f.Journal = j
	u, e := j.Usage(ctx)
	if e != nil || u.Bytes > c.LogBytes || u.Files > c.LogFiles {
		return f, observability.ErrCapacity
	}
	if e = j.Flush(ctx); e != nil {
		return f, e
	}
	if c.Service.Role == config.Worker {
		for _, entry := range []struct {
			path  string
			limit int64
			dst   **staging.QuotaFiles
		}{{c.StagingDirectory, c.StagingBytes, &f.Staging}, {c.TargetDirectory, c.TargetBytes, &f.Targets}} {
			d, e := staging.OpenDirectory(entry.path)
			if e != nil {
				return f, e
			}
			closers = append(closers, d.Close)
			if e = d.Sync(ctx); e != nil {
				return f, e
			}
			used, e := d.Usage(ctx)
			if e != nil || (entry.limit > 0 && used > entry.limit) {
				return f, staging.ErrCapacity
			}
			*entry.dst, e = staging.NewQuotaFiles(d, entry.limit)
			if e != nil {
				return f, e
			}
		}
	}
	return f, ctx.Err()
}
