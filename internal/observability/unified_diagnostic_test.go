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

type partialErrorBody struct{ sent bool }

func (b *partialErrorBody) Read(p []byte) (int, error) {
	if b.sent {
		return 0, io.ErrUnexpectedEOF
	}
	b.sent = true
	return copy(p, "gateway read interrupted"), io.ErrUnexpectedEOF
}
func (*partialErrorBody) Close() error { return nil }

func TestUnifiedEvidenceSurvivesEveryFailureStage(t *testing.T) {
	for _, tc := range []struct {
		name, stage, explanation, body string
		status                         int
		transport, partial             bool
	}{
		{name: "unfamiliar transport", stage: "transport", explanation: "novel proxy failure", transport: true},
		{name: "unfamiliar HTTP", stage: "http", status: 599, body: `{"code":"PROVIDER_NEW","problem":{"reason":"novel provider failure"},"data":{"XM":"Private Name"}}`, explanation: "novel provider failure"},
		{name: "unknown business", stage: "business", status: 200, body: `{"code":98765,"error_description":"new vendor error"}`, explanation: "new vendor error"},
		{name: "invalid envelope", stage: "decode", status: 200, body: `{"data":{"XM":"Private Name"`, explanation: "invalid object"},
		{name: "invalid data", stage: "validate", status: 200, body: `{"code":200,"message":"ok","data":{"total":-1,"list":[]}}`, explanation: "non-negative integer"},
		{name: "empty HTTP", stage: "http", status: 502, explanation: "http"},
		{name: "partial body", stage: "read", status: 200, partial: true, explanation: "unexpected EOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sanitizer(t)
			db := &logsDB{rows: map[string]string{}}
			repo := observability.Repository{Registry: logRegistry(t), Database: db}
			guard, _ := observability.NewGuard(s, observability.SafeSinkFunc(func(ctx context.Context, e observability.SafeEvent) error {
				return repo.StoreBatch(ctx, []observability.SafeEvent{e})
			}))
			r, _ := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), guard, time.Second)
			c, err := ingest.NewClient("https://synthetic.invalid", "dynamic-token", authTransport(func(req *http.Request) (*http.Response, error) {
				if tc.transport {
					return nil, errors.New("novel proxy failure: " + req.URL.String() + " dynamic-token fictional-secret-token")
				}
				var body io.ReadCloser = io.NopCloser(strings.NewReader(tc.body))
				if tc.partial {
					body = &partialErrorBody{}
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"X-Request-Id": {"upstream-new"}}, Body: body}, nil
			}), 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			input := ingest.PageRequest{Table: "dwd_bzksbjsjzlb", Mode: ingest.Incremental, Page: 7, PageSize: 50, StartTime: "2026-10-14 08:00:00", EndTime: "2026-10-14 10:00:00"}
			result, err := c.FetchPage(events.WithRecorder(t.Context(), r, events.Identity{TaskID: "unified-task"}), input, ingest.RetryPolicy{MaxAttempts: 1})
			if err == nil || result.ObservationError != nil {
				t.Fatalf("failure evidence missing: %v %v", err, result.ObservationError)
			}
			reader := observability.TaskDiagnostics{Repository: repo, Sanitizer: s}
			rows, err := reader.FailedAttempts(t.Context(), "unified-task")
			if err != nil || len(rows) != 1 {
				t.Fatalf("read failed: %v", err)
			}
			row := rows[0]
			exportFrontendDiagnostic(t, row, db, s)
			if row.TaskID != s.Correlation("unified-task") || row.CallID == "" || row.AttemptID == "" || row.Path == "" || row.StartTime != input.StartTime || row.EndTime != input.EndTime || row.PageSize != 50 || row.Page != 7 || row.Mode != "incremental" || len(row.Diagnostics) != 1 {
				t.Fatal("correlation/window lost")
			}
			d := row.Diagnostics[0]
			if d.HTTPStatus != tc.status || !d.Sent || d.Method != "GET" || d.Stage != tc.stage || d.StartedAt.IsZero() || d.DurationNS < 0 || !strings.Contains(d.Endpoint, row.Path) || !strings.Contains(d.Error+d.Response.Body, tc.explanation) {
				t.Fatal("request facts lost")
			}
			if tc.transport && (d.Response != (diagnostic.Response{}) || d.ErrorType == "") {
				t.Fatal("fabricated response or missing error type")
			}
			if tc.partial && !d.Response.ReadError {
				t.Fatal("partial body presented as complete")
			}
			if tc.name == "empty HTTP" && d.Response.Unavailable != "empty" {
				t.Fatal("empty response not distinguished")
			}
			if tc.name == "unknown business" && d.BusinessCode != "98765" {
				t.Fatal("business code lost")
			}
			for _, raw := range db.rows {
				for _, secret := range []string{"dynamic-token", "fictional-secret-token", "Private Name", "access_token="} {
					if strings.Contains(raw, secret) {
						t.Fatal("sensitive evidence persisted")
					}
				}
			}
		})
	}
}

func TestUnknownClassificationAndLargeEvidenceRemainReadable(t *testing.T) {
	s := sanitizer(t)
	d := diagnostic.Request{Kind: "authentication", Method: "GET", Endpoint: "https://synthetic.invalid" + diagnostic.TokenEndpoint, StartedAt: time.Now().UTC(), Sent: true, HTTPStatus: 502, Stage: "future_stage", Failure: "new_failure", Error: strings.Repeat("x", 512), Response: diagnostic.Response{Body: strings.Repeat("<", 2048)}}
	var saved observability.SafeEvent
	r, _ := events.New(testkit.FakeClock{Time: time.Now()}, testkit.FixedRandom(1), events.SinkFunc(func(_ context.Context, e events.Event) error {
		var err error
		saved, err = s.Sanitize(e, observability.Details{})
		return err
	}), time.Second)
	ctx, span, err := r.Begin(t.Context(), events.Call, events.Identity{TaskID: "task"}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = span.FinishDiagnostics(ctx, events.Failed, nil, nil, "new_failure", []diagnostic.Request{d, d}); err != nil {
		t.Fatal(err)
	}
	if len(saved.Bytes()) > 4096 || saved.Event().Metadata.Failure != "unknown" || len(saved.Event().Diagnostics) != 2 {
		t.Fatal("unknown/large evidence dropped")
	}
	for _, request := range saved.Event().Diagnostics {
		if !request.Response.Truncated || request.HTTPStatus != 502 || request.Failure != "new_failure" {
			t.Fatal("truncation dropped essential facts")
		}
	}
	if _, err := s.Restore(saved.Bytes()); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(saved.Event())
	if strings.Contains(string(encoded), `"authentication":`) {
		t.Fatal("duplicated legacy payload")
	}
}
