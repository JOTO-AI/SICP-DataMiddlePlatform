package ingest

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
)

// RetryPolicy.Wait receives the completed attempt number. The production
// budget/backoff boundary supplies it; tests can control it without sleeping.
// Transports must not hide additional application retries inside RoundTrip.
type RetryPolicy struct {
	MaxAttempts int
	Wait        func(context.Context, int) error
}

type AttemptResult struct {
	Number  int
	Attempt Attempt
	Failure FailureKind
}

type PageResult struct {
	Page     Page
	Attempts []AttemptResult
}

const RetryWaitFailure FailureKind = "retry_wait"

// FetchPage only publishes a complete, validated page once. Failed attempts
// contribute telemetry, never rows. There are no persistence side effects.
func (c *Client) FetchPage(ctx context.Context, input PageRequest, policy RetryPolicy) (PageResult, error) {
	return fetchPage(ctx, input, policy, c.Fetch)
}

func fetchPage(ctx context.Context, input PageRequest, policy RetryPolicy, fetch func(context.Context, PageRequest) (Page, error)) (PageResult, error) {
	result := PageResult{}
	if policy.MaxAttempts < 1 || policy.MaxAttempts > 100 || (policy.MaxAttempts > 1 && policy.Wait == nil) {
		return result, &Failure{Kind: ConfigurationFailure}
	}
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		page, err := fetch(ctx, input)
		if err == nil {
			result.Attempts = append(result.Attempts, AttemptResult{Number: attempt, Attempt: page.Attempt})
			result.Page = page
			return result, nil
		}
		var failure *Failure
		if !errors.As(err, &failure) {
			return result, &Failure{Kind: TransportFailure}
		}
		result.Attempts = append(result.Attempts, AttemptResult{Number: attempt, Attempt: failure.Attempt, Failure: failure.Kind})
		if !failure.retryable || attempt == policy.MaxAttempts {
			return result, failure
		}
		if err := policy.Wait(ctx, attempt); err != nil {
			var budgetFailure *Failure
			if errors.As(err, &budgetFailure) {
				return result, budgetFailure
			}
			if ctx.Err() != nil {
				return result, &Failure{Kind: CancelledFailure, Cause: ctx.Err()}
			}
			return result, &Failure{Kind: RetryWaitFailure}
		}
	}
	panic("unreachable retry loop")
}

// Keep only the classification, never the original transport error: net/url
// and TLS errors can contain credentials or an entire sensitive request URL.
func temporaryTransport(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var network net.Error
	return errors.As(err, &network) && (network.Timeout() || network.Temporary())
}
