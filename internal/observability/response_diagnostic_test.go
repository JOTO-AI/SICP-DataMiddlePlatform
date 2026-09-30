package observability_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 500, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"upstream-trace"}}, Body: io.NopCloser(strings.NewReader(`{"message":"database unavailable; fictional-secret-token dynamic-token","data":{"XM":"Private Name"}}`))}, nil
}

type noCooldown struct{}

func (noCooldown) Cooldown(context.Context) (time.Time, error)     { return time.Time{}, nil }
func (noCooldown) ExtendCooldown(context.Context, time.Time) error { return nil }

func TestEveryRetriedErrorSurvivesSanitizationPersistenceAndTaskLookup(t *testing.T) {
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
	c, err := ingest.NewClient("https://synthetic.invalid", "dynamic-token", errorTransport{}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	limiter, _ := ingest.NewLimiter(1)
	collector, err := ingest.NewCoordinator(c, ingest.BudgetConfig{RequestTimeout: time.Second, PageTimeout: time.Minute, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond, MaxAttempts: 3, RetryHTTP: []int{500}}, ingest.SystemTimer{}, testkit.FixedRandom(0), noCooldown{}, limiter)
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := ingest.NewTaskBudget(time.Now().Add(time.Minute), 100)
	ctx := events.WithRecorder(context.Background(), r, events.Identity{TaskID: "failed-task"})
	_, err = collector.FetchPage(ctx, ingest.PageRequest{Table: "dwd_bzkscjzlb", Mode: ingest.Full, Page: 1115, PageSize: 1000}, budget)
	if err == nil {
		t.Fatal("failure lost")
	}
	reader := observability.TaskDiagnostics{Repository: observability.Repository{Registry: logRegistry(t), Database: db}, Sanitizer: s}
	rows, err := reader.FailedAttempts(context.Background(), "failed-task")
	if err != nil || len(rows) != 3 {
		t.Fatal("attempt evidence not persisted", err, len(rows))
	}
	seen := map[int]bool{}
	for _, row := range rows {
		seen[row.Attempt] = true
		if row.Page != 1115 || row.HTTPStatus == nil || *row.HTTPStatus != 500 || row.Response == nil || !strings.Contains(row.Response.Body, "database unavailable") || row.Response.RequestID != "upstream-trace" {
			t.Fatal("diagnostic lost")
		}
	}
	if !seen[1] || !seen[2] || !seen[3] {
		t.Fatal("retry attempts collapsed")
	}
	for _, raw := range db.rows {
		for _, secret := range []string{"fictional-secret-token", "dynamic-token", "Private Name"} {
			if strings.Contains(raw, secret) {
				t.Fatal("secret persisted")
			}
		}
	}
	other, err := reader.FailedAttempts(context.Background(), "other-task")
	if err != nil || len(other) != 0 {
		t.Fatal("cross-task leakage")
	}
	db.lostReply = true
	if partial, err := reader.FailedAttempts(context.Background(), "failed-task"); err == nil || partial != nil {
		t.Fatal("partial result escaped failed read transaction")
	}
}
