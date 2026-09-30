package candidate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

type blockedBaseline struct {
	CompleteSource
	entered chan string
	release chan struct{}
	fail    bool
	active  atomic.Int64
}

func (b *blockedBaseline) Open(ctx context.Context, m Manifest, v TableVersion) (Rows, error) {
	b.active.Add(1)
	defer b.active.Add(-1)
	select {
	case b.entered <- v.Table:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if b.fail && v.Table == "dwd_bzksbjsjzlb" {
		return nil, errors.New("injected table failure")
	}
	return b.CompleteSource.Open(ctx, m, v)
}

func TestParallelTablesMatchSerialOutputAndKeepGlobalChunkLimit(t *testing.T) {
	input, files, options, _ := targetInput(t, ingest.Full, `{"ID":"b"}`, `{"ID":"a"}`, `{"ID":"c"}`)
	serial, err := files.Prepare(t.Context(), options, input)
	if err != nil {
		t.Fatal(err)
	}
	options.TableConcurrency = 4
	parallel, err := files.Prepare(t.Context(), options, input)
	if err != nil || !reflect.DeepEqual(serial, parallel) {
		t.Fatal("concurrency changed immutable target", err)
	}
	options.Combination = "too-small"
	files.MaxChunks = 1
	if _, err = files.Prepare(t.Context(), options, input); !errors.Is(err, staging.ErrCapacity) {
		t.Fatal("global chunk limit bypassed", err)
	}
}

func TestTwoTableBuildsOverlapAndFailuresJoinBeforeReturning(t *testing.T) {
	for _, fail := range []bool{false, true} {
		baseInput, baseFiles, baseOptions, _ := targetInput(t, ingest.Full)
		base, err := baseFiles.Prepare(t.Context(), baseOptions, baseInput)
		if err != nil {
			t.Fatal(err)
		}
		input, files, options, root := targetInput(t, ingest.Incremental)
		blocked := &blockedBaseline{CompleteSource: baseFiles, entered: make(chan string, 14), release: make(chan struct{}), fail: fail}
		options.Combination = "C2"
		options.Base = &base
		options.BaseSource = blocked
		options.TableConcurrency = 2
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := files.Prepare(ctx, options, input); done <- err }()
		for range 2 {
			select {
			case <-blocked.entered:
			case <-time.After(5 * time.Second):
				cancel()
				<-done
				t.Fatal("tables did not start concurrently")
			}
		}
		if blocked.active.Load() != 2 {
			cancel()
			<-done
			t.Fatal("parallel bound incorrect")
		}
		close(blocked.release)
		err = <-done
		cancel()
		if (err != nil) != fail || blocked.active.Load() != 0 {
			t.Fatal("workers did not join", err)
		}
		if fail {
			if _, err = os.Stat(filepath.Join(root, manifestName("C2"))); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("partial group published manifest", err)
			}
		}
	}
}
