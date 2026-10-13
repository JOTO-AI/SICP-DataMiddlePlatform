package operations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type sourceTransport func(*http.Request) (*http.Response, error)

func (f sourceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type sourceCooldown struct{ until time.Time }

func (s *sourceCooldown) Cooldown(context.Context) (time.Time, error) { return s.until, nil }
func (s *sourceCooldown) ExtendCooldown(_ context.Context, at time.Time) error {
	s.until = at
	return nil
}

type sourceBody struct {
	io.Reader
	closed bool
}

func (b *sourceBody) Close() error { b.closed = true; return nil }

func TestT32_11And12And15ControlledSourceExecution(t *testing.T) {
	for _, mode := range []string{"success", "business_failure", "http_failure", "oversized", "redirect", "timeout", "cooldown", "already_expired", "lost_owner"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				calls := 0
				finalStatus := ""
				var summary string
				observed := []events.Event{}
				job := Job{ID: "test-run", Subject: "actor", Action: "source_test", Status: "queued", AcceptedAt: start.Add(-20 * time.Second), Deadline: start.Add(10 * time.Second), Request: json.RawMessage(`{"api_id":"dwd_xqjbsjzlb","version":"` + CatalogVersion + `","page":3,"page_size":10}`), Summary: json.RawMessage(`{}`)}
				if mode == "already_expired" {
					job.Deadline = start.Add(4 * time.Second)
				}
				tx := &fakeTx{row: func(q string, a []any) storage.Row {
					if strings.Contains(q, "SELECT (SELECT id") {
						return fakeRow(func(d ...any) error { *d[0].(**string) = &job.ID; return nil })
					}
					if strings.Contains(q, "EXISTS(SELECT 1 FROM control.tasks") {
						return fakeRow(func(d ...any) error { *d[0].(*bool) = false; return nil })
					}
					return jobRow(job)
				}, exec: func(q string, a []any) (int64, error) {
					if strings.Contains(q, "status=$3,summary=") {
						finalStatus = a[2].(string)
						summary = a[3].(string)
						if mode == "lost_owner" {
							return 0, nil
						}
					}
					if strings.Contains(q, "INSERT INTO control.tasks") || strings.Contains(q, "UPDATE control.source_progress") {
						t.Fatal("test changed synchronization")
					}
					return 1, nil
				}}
				body := &sourceBody{Reader: strings.NewReader(`{"code":200,"data":{"total":0,"list":[]}}`)}
				client, err := ingest.NewClient("https://fixed.invalid", "synthetic-secret", sourceTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.URL.Host != "fixed.invalid" || r.URL.Path != "/apizt/openapi/invoke/sjzc/release/dwd_xqjbsjzlb" || len(r.URL.Query()) != 3 || r.URL.Query().Get("page") != "3" || r.URL.Query().Get("pageSize") != "10" || r.URL.Query().Get("access_token") != "synthetic-secret" {
						t.Fatal("target or parameter injection")
					}
					if mode == "timeout" {
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
					status := 200
					headers := http.Header{}
					switch mode {
					case "business_failure":
						body.Reader = strings.NewReader(`{"code":500,"message":"synthetic failure"}`)
					case "http_failure":
						status = 503
					case "oversized":
						body.Reader = io.LimitReader(strings.NewReader(strings.Repeat("x", (2<<20)+1)), (2<<20)+1)
					case "redirect":
						status = 302
						headers.Set("Location", "https://injected.invalid")
					}
					return &http.Response{StatusCode: status, Header: headers, Body: body}, nil
				}), 2<<20)
				if err != nil {
					t.Fatal(err)
				}
				cooldown := &sourceCooldown{}
				if mode == "cooldown" {
					cooldown.until = start.Add(time.Minute)
				}
				limiter, _ := ingest.NewLimiter(1)
				collector, err := ingest.NewCoordinator(client, ingest.BudgetConfig{RequestTimeout: 30 * time.Second, PageTimeout: 30 * time.Second, InitialBackoff: time.Second, MaxBackoff: time.Second, MaxAttempts: 1}, ingest.SystemTimer{}, testkit.FixedRandom(0), cooldown, limiter.LowPriority())
				if err != nil {
					t.Fatal(err)
				}
				recorder, err := events.New(ingest.SystemTimer{}, testkit.FixedRandom(0), events.SinkFunc(func(_ context.Context, e events.Event) error { observed = append(observed, e); return nil }), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				runner := &TestRunner{Repository: fixtureRepo(tx), Collector: collector, Recorder: recorder, Results: NewResults(), Owner: "worker"}
				runner.running.Store(true)
				if err := runner.Pump(t.Context()); err != ErrLimit {
					t.Fatal("concurrent execution accepted", err)
				}
				runner.running.Store(false)
				err = runner.Pump(t.Context())
				if mode == "lost_owner" {
					if err != ErrConflict {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				want := "failed"
				if mode == "success" || mode == "lost_owner" {
					want = "succeeded"
				}
				if finalStatus != want || calls > 1 || strings.Contains(summary, "synthetic-secret") || strings.Contains(summary, `"list"`) {
					t.Fatal("incorrect durable result", finalStatus, summary, calls)
				}
				_, available := runner.Results.Get(job.ID, "actor", time.Now())
				if available != (mode == "success") {
					t.Fatal("raw results escaped failed/unowned execution")
				}
				if mode == "cooldown" || mode == "already_expired" {
					if calls != 0 {
						t.Fatal("expired/cooling target contacted")
					}
				} else if calls != 1 {
					t.Fatal("source was not attempted exactly once")
				}
				if mode == "timeout" && time.Since(start) != 5*time.Second {
					t.Fatal("execution ignored queue time or persistence reserve")
				}
				if calls > 0 && mode != "timeout" && !body.closed {
					t.Fatal("response body leaked")
				}
				for _, e := range observed {
					if e.Identity.TaskID != "" || e.Identity.Purpose != events.TestPurpose || e.Identity.TestRunID != job.ID || (e.Kind != events.Call && e.Kind != events.Attempt) {
						t.Fatal("incorrect event association")
					}
				}
			})
		})
	}
}
