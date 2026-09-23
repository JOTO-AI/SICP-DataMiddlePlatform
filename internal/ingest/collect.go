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
	result.Manifest.Source = input.Table
	pageNumber := input.Page
	defer func() {
		if finalErr != nil {
			result.Manifest.Complete = false
			finalErr = &AcquisitionError{Source: input.Table, Page: pageNumber, Cause: finalErr}
		}
	}()
	if c == nil || task == nil || sink == nil || input.Page != 1 {
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
		input.Page = pageNumber
		page, err := c.FetchPage(taskCtx, input, task)
		if page.ObservationError != nil {
			result.ObservationError = page.ObservationError
		}
		result.Calls = append(result.Calls, PageCall{Page: pageNumber, Attempts: page.Attempts})
		if err != nil {
			result.FinalFailure = page.FinalFailure
			return result, err
		}
		if err := pager.Accept(pageNumber, page.Page); err != nil {
			return result, err
		}
		if err := check(); err != nil {
			return result, err
		}
		summary := pager.manifest.Pages[len(pager.manifest.Pages)-1]
		if err := sink.CommitPage(taskCtx, input, page.Page, summary); err != nil {
			// A storage error can include local paths/secrets. Do not expose it
			// through a public acquisition error or trigger more source requests.
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
