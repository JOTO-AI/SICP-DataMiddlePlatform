package observability_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type authTransport func(*http.Request) (*http.Response, error)

func (f authTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthenticationFailureSurvivesPersistenceAndTaskLookup(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial_token_502", true: "page_401_then_token_502"}[refresh], func(t *testing.T) {
			s := sanitizer(t)
			db := &logsDB{rows: map[string]string{}}
			repo := observability.Repository{Registry: logRegistry(t), Database: db}
			guard, err := observability.NewGuard(s, observability.SafeSinkFunc(func(ctx context.Context, e observability.SafeEvent) error {
				return repo.StoreBatch(ctx, []observability.SafeEvent{e})
			}))
			if err != nil {
				t.Fatal(err)
			}
			r, err := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), guard, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			authCalls, pageCalls := 0, 0
			c, err := ingest.NewAuthenticatedClient("https://synthetic.invalid", "private-app-id", "private-secret", authTransport(func(req *http.Request) (*http.Response, error) {
				status, body := 502, "private-secret private-app-id private-token"
				if req.URL.Path == diagnostic.TokenEndpoint {
					authCalls++
					if refresh && authCalls == 1 {
						status, body = 200, `{"code":200,"data":{"access_token":"private-token"}}`
					}
				} else {
					pageCalls++
					status, body = 401, "unauthorized"
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			}), 1<<20, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			ctx := events.WithRecorder(t.Context(), r, events.Identity{TaskID: "auth-failed-task"})
			limiter, _ := ingest.NewLimiter(1)
			collector, err := ingest.NewCoordinator(c, ingest.BudgetConfig{RequestTimeout: time.Second, PageTimeout: time.Minute, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond, MaxAttempts: 1}, ingest.SystemTimer{}, testkit.FixedRandom(0), noCooldown{}, limiter)
			if err != nil {
				t.Fatal(err)
			}
			budget, _ := ingest.NewTaskBudget(time.Now().Add(time.Minute), 10)
			result, err := collector.FetchPage(ctx, ingest.PageRequest{Table: "dwd_bzksbjsjzlb", Mode: ingest.Full, Page: 1, PageSize: 2000}, budget)
			var failure *ingest.Failure
			if !errors.As(err, &failure) || failure.Kind != ingest.AuthenticationFailure || result.ObservationError != nil || len(db.rows) != 4 {
				t.Fatalf("authentication failure/end events lost: err=%v observation=%v events=%d", err, result.ObservationError, len(db.rows))
			}
			if (!refresh && (pageCalls != 0 || authCalls != 1)) || (refresh && (pageCalls != 1 || authCalls != 2)) {
				t.Fatal("authentication changed request/retry behavior")
			}
			reader := observability.TaskDiagnostics{Repository: repo, Sanitizer: s}
			rows, err := reader.FailedAttempts(t.Context(), "auth-failed-task")
			if err != nil || len(rows) != 1 {
				t.Fatal("missing failed attempt", err, len(rows))
			}
			row := rows[0]
			want := diagnostic.Authentication{Endpoint: diagnostic.TokenEndpoint, Reason: "http", HTTPStatus: 502}
			if row.Failure != "authentication" || row.Authentication == nil || *row.Authentication != want {
				t.Fatal("token endpoint/HTTP failure missing from task diagnostics")
			}
			if (!refresh && (row.HTTPStatus != nil || row.Response != nil)) || (refresh && (row.HTTPStatus == nil || *row.HTTPStatus != 401)) {
				t.Fatal("token response incorrectly replaced data-page evidence")
			}
			ends := 0
			for _, raw := range db.rows {
				for _, secret := range []string{"private-app-id", "private-secret", "private-token"} {
					if strings.Contains(raw, secret) {
						t.Fatal("credential or auth response persisted")
					}
				}
				var saved struct {
					Event events.Event `json:"event"`
				}
				if err := json.Unmarshal([]byte(raw), &saved); err != nil {
					t.Fatal(err)
				}
				if saved.Event.Phase == events.Finished {
					ends++
					if saved.Event.Metadata.Authentication == nil || *saved.Event.Metadata.Authentication != want {
						t.Fatal("call/attempt detail lost auth evidence")
					}
				}
			}
			if ends != 2 {
				t.Fatal("call and attempt must both finish")
			}
		})
	}
}

func TestAuthenticationRequestDeadlineStillRecordsBothEnds(t *testing.T) {
	var saved []events.Event
	r, err := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), events.SinkFunc(func(_ context.Context, e events.Event) error {
		if err := e.Validate(); err != nil {
			return err
		}
		saved = append(saved, e)
		return nil
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ingest.NewAuthenticatedClient("https://synthetic.invalid", "private-app-id", "private-secret", authTransport(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}), 1<<20, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	limiter, _ := ingest.NewLimiter(1)
	collector, err := ingest.NewCoordinator(c, ingest.BudgetConfig{RequestTimeout: 20 * time.Millisecond, PageTimeout: time.Minute, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond, MaxAttempts: 1}, ingest.SystemTimer{}, testkit.FixedRandom(0), noCooldown{}, limiter)
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := ingest.NewTaskBudget(time.Now().Add(time.Minute), 10)
	ctx := events.WithRecorder(t.Context(), r, events.Identity{TaskID: "auth-timeout-task"})
	result, err := collector.FetchPage(ctx, ingest.PageRequest{Table: "dwd_bzksbjsjzlb", Mode: ingest.Full, Page: 1, PageSize: 2000}, budget)
	var failure *ingest.Failure
	if !errors.As(err, &failure) || failure.Kind != ingest.RequestTimeout || result.ObservationError != nil || len(saved) != 4 {
		t.Fatalf("deadline evidence lost: %v observation=%v events=%d", err, result.ObservationError, len(saved))
	}
	for _, e := range saved[2:] {
		if e.Phase != events.Finished || e.Metadata.Failure != "request_timeout" || e.Metadata.Authentication == nil || e.Metadata.Authentication.Reason != "timeout" || e.Metadata.Authentication.HTTPStatus != 0 || e.HTTPStatus != nil {
			t.Fatal("request timeout lost token endpoint context or invented a response")
		}
	}
}
