package update

import (
	"context"
	"errors"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

type failingProgressSink struct{}

func (failingProgressSink) CommitPage(context.Context, ingest.PageRequest, ingest.Page, ingest.PageSummary) error {
	return errors.New("write failed")
}

func TestFailedPageCommitDoesNotAdvanceVisibleProgress(t *testing.T) {
	called := false
	s := acquisitionSink{PageSink: failingProgressSink{}, afterCommit: func(context.Context, ingest.PageRequest, ingest.Page) { called = true }}
	if err := s.CommitPage(t.Context(), ingest.PageRequest{}, ingest.Page{}, ingest.PageSummary{}); err == nil || called {
		t.Fatal("uncommitted page was counted")
	}
}
