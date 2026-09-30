package ingest

import "context"

const PageCommitFailure FailureKind = "page_commit"

// PageSink confirms one complete page. Implementations must honor cancellation
// and make repeated confirmation idempotent; P04 supplies durable staging.
type PageSink interface {
	CommitPage(context.Context, PageRequest, Page, PageSummary) error
}
type PageCall struct {
	Page     int
	Attempts []AttemptResult
}
type Acquisition struct {
	Manifest         Manifest
	Calls            []PageCall
	FinalFailure     *FailureEvent
	ObservationError error
}
type AcquisitionError struct {
	Source string
	Page   int
	Cause  error
}

func (e *AcquisitionError) Error() string    { return "source acquisition failed" }
func (e *AcquisitionError) GoString() string { return e.Error() }
func (e *AcquisitionError) Unwrap() error    { return e.Cause }

// Collect always starts at page one. It returns only confirmed page evidence;
// interrupted acquisitions cannot be resumed by mixing pages with a new run.
func (c *Coordinator) Collect(ctx context.Context, input PageRequest, task *TaskBudget, limits PageLimits, sink PageSink) (result Acquisition, finalErr error) {
	return c.CollectConcurrent(ctx, input, task, limits, sink, 1)
}

// CollectConcurrent fetches bounded windows after confirming page one. A window
// is joined before ordered validation/commit, so sinks and progress remain
// serial and at most concurrency response pages await processing.
func (c *Coordinator) CollectConcurrent(ctx context.Context, input PageRequest, task *TaskBudget, limits PageLimits, sink PageSink, concurrency int) (result Acquisition, finalErr error) {
	result.Manifest.Source = input.Table
	pageNumber := input.Page
	defer func() {
		if finalErr != nil {
			result.Manifest.Complete = false
			finalErr = &AcquisitionError{Source: input.Table, Page: pageNumber, Cause: finalErr}
		}
	}()
	if c == nil || task == nil || sink == nil || input.Page != 1 || concurrency < 1 || concurrency > MaxPageConcurrency {
		return result, &Failure{Kind: ConfigurationFailure}
	}
	pager, err := NewPager(input.Table, input.PageSize, limits)
	if err != nil {
		return result, err
	}
	deadline := task.deadline
	if outer, ok := ctx.Deadline(); ok {
		deadline = earlier(deadline, outer)
	}
	taskCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	check := func() error {
		if err := taskCtx.Err(); err != nil {
			return &Failure{Kind: CancelledFailure, Cause: err}
		}
		if !c.clock.Now().Before(deadline) {
			return &Failure{Kind: BudgetFailure}
		}
		return nil
	}
	for {
		if err := check(); err != nil {
			return result, err
		}
		count := 1
		if pageNumber > 1 {
			total := pager.manifest.Total / int64(input.PageSize)
			if pager.manifest.Total%int64(input.PageSize) != 0 {
				total++
			}
			count = int(min(int64(concurrency), total-int64(pageNumber)+1))
		}
		input.Page = pageNumber
		batch, failed := c.fetchWindow(taskCtx, input, task, count)
		for i, page := range batch {
			if page.result.ObservationError != nil {
				result.ObservationError = page.result.ObservationError
			}
			result.Calls = append(result.Calls, PageCall{Page: pageNumber + i, Attempts: page.result.Attempts})
		}
		if failed >= 0 {
			pageNumber += failed
			result.FinalFailure = batch[failed].result.FinalFailure
			return result, batch[failed].err
		}
		for _, page := range batch {
			input.Page = pageNumber
			if err := check(); err != nil {
				return result, err
			}
			if err := pager.Accept(pageNumber, page.result.Page); err != nil {
				return result, err
			}
			if err := check(); err != nil {
				return result, err
			}
			summary := pager.manifest.Pages[len(pager.manifest.Pages)-1]
			if err := sink.CommitPage(taskCtx, input, page.result.Page, summary); err != nil {
				return result, &Failure{Kind: PageCommitFailure}
			}
			result.Manifest = pager.Manifest()
			if err := check(); err != nil {
				return result, err
			}
			if result.Manifest.Complete {
				return result, nil
			}
			pageNumber++
		}
	}
}
