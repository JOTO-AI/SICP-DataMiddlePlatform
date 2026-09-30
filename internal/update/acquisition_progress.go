package update

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type acquisitionSink struct {
	ingest.PageSink
	afterCommit func(context.Context, ingest.PageRequest, ingest.Page)
}

func (s acquisitionSink) CommitPage(ctx context.Context, request ingest.PageRequest, page ingest.Page, summary ingest.PageSummary) error {
	if err := s.PageSink.CommitPage(ctx, request, page, summary); err != nil {
		return err
	}
	s.afterCommit(ctx, request, page)
	return nil
}

func (a Acquirer) reportProgress(ctx context.Context, tables []control.TableAcquisitionProgress) {
	if a.Progress == nil {
		return
	}
	// A telemetry failure does not discard successfully retained source pages.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.Progress(ctx, tables); err != nil {
		a.Observe(err)
	}
}
