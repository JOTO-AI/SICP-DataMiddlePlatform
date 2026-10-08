//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/worker"
)

// The real durable event marks an exact consumer boundary, so this fault does
// not substitute a fake file reader or PostgreSQL writer into the build path.
type targetMutationSink struct {
	events.Sink
	root, table, phase string
	once               sync.Once
	file               string
	original           []byte
	err                error
}

func (s *targetMutationSink) Record(ctx context.Context, e events.Event) error {
	if err := s.Sink.Record(ctx, e); err != nil {
		return err
	}
	if e.Metadata.Build == nil || e.Metadata.Build.Stage != "write_table" || e.Metadata.Build.Table != s.table || string(e.Phase) != s.phase || string(e.Identity.Instance) != "M" {
		return nil
	}
	if e.Phase == events.Finished && e.Outcome != events.Succeeded {
		return nil
	}
	s.once.Do(func() {
		files, err := filepath.Glob(filepath.Join(s.root, "target-Cfile-tamper.dwd_bzksbjsjzlb.*.jsonl"))
		if err != nil || len(files) != 1 {
			s.err = errors.New("expected one synthetic class chunk")
			return
		}
		s.file = files[0]
		s.original, s.err = os.ReadFile(s.file)
		if s.err != nil {
			return
		}
		// The previously consumed contents still exist in PostgreSQL; alter the
		// immutable disk artifact after its digest was already checked.
		s.err = os.WriteFile(s.file, append(append([]byte(nil), s.original...), '\n'), 0600)
	})
	return s.err
}

func TestI05_B10TargetTamperingAtConsumerBoundariesCannotPublish(t *testing.T) {
	for _, boundary := range []struct{ name, table, phase string }{
		{"before_read", "dwd_bzksbjsjzlb", "started"},
		{"after_read", "dwd_bzksbjsjzlb", "finished"},
		{"before_final_check", "dwd_yxsdwjbsjzlb", "finished"},
	} {
		t.Run(boundary.name, func(t *testing.T) {
			f := newRuntime(t, false)
			executor := f.worker.Engine.Work.(worker.Executor)
			fault := &targetMutationSink{Sink: executor.Sink, root: f.wc.TargetDirectory, table: boundary.table, phase: boundary.phase}
			recorder, err := events.New(ports.SystemClock{}, rand.Reader, fault, 2*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			executor.Recorder = recorder
			f.worker.Engine.Work = executor
			admitTask(t, f, "initialize", "file-tamper")
			if err := f.worker.Engine.Cycle(f.e.ctx); err == nil || fault.err != nil || fault.file == "" || len(fault.original) == 0 {
				t.Fatal("target fault was not reached or not detected", err, fault.err)
			}
			if countControl(t, f, "SELECT count(*) FROM control.candidates WHERE state='ready'") != 0 ||
				countControl(t, f, "SELECT count(*) FROM control.publication_outbox") != 0 ||
				countControl(t, f, "SELECT count(*) FROM control.source_progress") != 0 {
				t.Fatal("tampered artifact authorized readiness or publication")
			}
			if _, exists, err := f.wr.Active.Read(f.e.ctx); err != nil || exists {
				t.Fatal("tampered initialization exposed active route", err)
			}
			if err := os.WriteFile(fault.file, fault.original, 0600); err != nil {
				t.Fatal(err)
			}
			// Restore the exact original bytes; retries must still establish both
			// copies independently from persisted state, without a second fault.
			if err := f.worker.Engine.Cycle(f.e.ctx); err != nil {
				t.Fatal("restored target recovery failed", err)
			}
			if task := loadTask(t, f, "file-tamper"); task.Status != "success_published" {
				t.Fatal(task.Status)
			}
			if countControl(t, f, "SELECT count(*) FROM control.candidates WHERE task_id='file-tamper' AND state='ready'") != 2 {
				t.Fatal("both copies were not revalidated")
			}
			assertProgress(t, f, "file-tamper")
		})
	}
}
