package ingest_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

type temporaryError struct{}

func (temporaryError) Error() string   { return "fictional-secret" }
func (temporaryError) Timeout() bool   { return true }
func (temporaryError) Temporary() bool { return true }

func retryPolicy() ingest.RetryPolicy {
	return ingest.RetryPolicy{MaxAttempts: 3, Wait: func(context.Context, int) error { return nil }}
}

func TestT03_10TransientFailuresRetryWholePageWithIdenticalParameters(t *testing.T) {
	for _, mode := range []ingest.Mode{ingest.Full, ingest.ForceFull, ingest.Incremental} {
		for _, truncate := range []bool{false, true} {
			calls, waits, accepted := 0, 0, 0
			originalURL := ""
			var bodies []*trackedBody
			c := sourceClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					originalURL = r.URL.String()
				} else if originalURL != r.URL.String() {
					t.Fatal("retry parameters changed")
				}
				q := r.URL.Query()
				if mode == ingest.Incremental {
					if q.Get("startTime") != "2026-08-05 07:55:00" || q.Get("endTime") != "2026-08-05 10:00:00" {
						t.Fatal("incremental window changed")
					}
				} else if q.Has("startTime") || q.Has("endTime") {
					t.Fatal("full request included time")
				}
				if calls == 1 && !truncate {
					return nil, temporaryError{}
				}
				var reader io.Reader = strings.NewReader(`{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":"01"}]}}`)
				if calls == 1 {
					reader = io.MultiReader(strings.NewReader(`{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":"01"}`), readError{})
				}
				b := &trackedBody{Reader: reader}
				bodies = append(bodies, b)
				return response(200, b), nil
			}))
			r := pageRequest()
			r.Mode = mode
			r.StartTime = "2026-08-05 07:55:00"
			r.EndTime = "2026-08-05 10:00:00"
			policy := retryPolicy()
			policy.Wait = func(_ context.Context, n int) error {
				waits++
				if n != 1 {
					t.Fatal("wrong wait attempt")
				}
				return nil
			}
			result, err := c.FetchPage(context.Background(), r, policy)
			if err == nil {
				accepted++
			}
			if err != nil || calls != 2 || waits != 1 || accepted != 1 || len(result.Page.Records) != 1 || result.Page.Records[0].Key != "01" || len(result.Attempts) != 2 {
				t.Fatalf("bad retry result: %v", err)
			}
			if result.Attempts[0].Failure != ingest.TransportFailure || result.Attempts[1].Failure != "" || result.Attempts[1].Number != 2 {
				t.Fatal("attempt history lost")
			}
			for _, b := range bodies {
				if !b.closed {
					t.Fatal("attempt body leaked")
				}
			}
		}
	}
}

func TestT03_11DeterministicFailuresDoNotRetry(t *testing.T) {
	for _, scenario := range []string{"token", "business", "parse", "structure", "key", "unknown", "permanent_network", "http"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				body := emptyResponse
				status := 200
				switch scenario {
				case "token":
					body = strings.Replace(body, "200", "401", 1)
				case "business":
					body = strings.Replace(body, "200", "999", 1)
				case "parse":
					body = `{"code":200`
				case "structure":
					body = `{"code":200,"message":"ok","data":null}`
				case "key":
					body = `{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":null}]}}`
				case "unknown":
					body = `{"code":200,"message":"ok","data":{"total":1,"list":[{"XQH":"01","UNKNOWN":null}]}}`
				case "permanent_network":
					return nil, errors.New("permanent error")
				case "http":
					status = 503 // No upstream retryable status contract configured yet.
				}
				return response(status, io.NopCloser(strings.NewReader(body))), nil
			}))
			policy := retryPolicy()
			policy.Wait = func(context.Context, int) error { t.Fatal("deterministic retry"); return nil }
			result, err := c.FetchPage(context.Background(), pageRequest(), policy)
			if err == nil || calls != 1 || len(result.Attempts) != 1 || result.Page.Records != nil {
				t.Fatal("deterministic failure retried or published")
			}
		})
	}
}

func TestT03_15AttemptLimitIncludesInitialRequest(t *testing.T) {
	for _, limit := range []int{1, 3} {
		calls, waits := 0, 0
		c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, temporaryError{} }))
		p := retryPolicy()
		p.MaxAttempts = limit
		p.Wait = func(context.Context, int) error { waits++; return nil }
		r, err := c.FetchPage(context.Background(), pageRequest(), p)
		if err == nil || calls != limit || waits != limit-1 || len(r.Attempts) != limit || r.Page.Records != nil {
			t.Fatal("incorrect attempt bound")
		}
	}
}

func TestT03_15InvalidPolicyAndCancelledBackoffStopCalls(t *testing.T) {
	calls := 0
	c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, temporaryError{} }))
	for _, p := range []ingest.RetryPolicy{{MaxAttempts: 0}, {MaxAttempts: 101}, {MaxAttempts: 2}} {
		if _, err := c.FetchPage(context.Background(), pageRequest(), p); err == nil || calls != 0 {
			t.Fatal("invalid policy sent request")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := retryPolicy()
	p.Wait = func(context.Context, int) error { cancel(); return ctx.Err() }
	r, err := c.FetchPage(ctx, pageRequest(), p)
	if !errors.Is(err, context.Canceled) || calls != 1 || len(r.Attempts) != 1 {
		t.Fatal("cancelled wait continued")
	}
}
