package staging_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

type payloadCounter struct {
	*memoryFiles
	reads int
}

func (f *payloadCounter) Read(ctx context.Context, name string, limit int64) ([]byte, error) {
	if strings.HasSuffix(name, ".jsonl") {
		f.reads++
	}
	return f.memoryFiles.Read(ctx, name, limit)
}

func TestVerifiedGroupDoesNotRescanPayloadsForEachTableAndExpires(t *testing.T) {
	_, memory, ref := completeStore(t)
	files := &payloadCounter{memoryFiles: memory}
	q, _ := staging.NewQuotaFiles(files, 0)
	s, _ := staging.NewStore(ref.RunID, q, 1<<20)
	l, err := s.Retain(t.Context(), ref, "build")
	if err != nil {
		t.Fatal(err)
	}
	var escaped context.Context
	err = l.WithVerifiedPages(t.Context(), 1<<20, 100, func(ctx context.Context) error {
		escaped = ctx
		before := files.reads
		for range 14 {
			if _, err := l.Bounded(ctx, 1<<20, 100); err != nil {
				return err
			}
		}
		if files.reads != before {
			t.Fatal("table entry repeated whole-input scan", files.reads-before)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.Bounded(escaped, 1<<20, 100); !errors.Is(err, staging.ErrInvalid) {
		t.Fatal("verification scope outlived build", err)
	}
}

func TestVerifiedGroupRejectsCorruptionBeforeItCanPublish(t *testing.T) {
	for _, readPage := range []bool{false, true} {
		s, files, ref := completeStore(t)
		l, err := s.Retain(t.Context(), ref, "build")
		if err != nil {
			t.Fatal(err)
		}
		err = l.WithVerifiedPages(t.Context(), 1<<20, 100, func(ctx context.Context) error {
			pages := l.Pages()
			entry := pages[len(pages)-1]
			files.files[entry.Payload] = []byte("corrupt")
			if readPage {
				if _, err := l.ReadPage(ctx, len(pages)-1); !errors.Is(err, staging.ErrCorrupt) {
					t.Fatal("page checksum bypassed", err)
				}
			}
			return nil
		})
		if !errors.Is(err, staging.ErrCorrupt) {
			t.Fatal("non-target corruption escaped final verification", err)
		}
	}
}
