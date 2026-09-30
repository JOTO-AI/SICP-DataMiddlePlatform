package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

func TestHTTPErrorCapturesRedactedBoundedBodyAndClosesIt(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader(`{"message":"upstream database unavailable fictional-secret","data":{"XM":"Private Name"}}`)}
	c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		r := response(500, body)
		r.Header = http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"trace-page-1115"}}
		return r, nil
	}))
	_, err := c.Fetch(context.Background(), pageRequest())
	var f *ingest.Failure
	if !errors.As(err, &f) || f.Kind != ingest.HTTPFailure || !body.closed || f.Attempt.Response == nil {
		t.Fatal("missing HTTP evidence or unclosed body")
	}
	raw, _ := json.Marshal(f.Attempt.Response)
	if strings.Contains(string(raw), "fictional-secret") || strings.Contains(string(raw), "Private Name") || !strings.Contains(string(raw), "database unavailable") || f.Attempt.Response.RequestID != "trace-page-1115" {
		t.Fatal("invalid diagnostic")
	}
}

type diagnosticReader struct{ n int }

func (r *diagnosticReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.n += len(p)
	return len(p), nil
}
func (*diagnosticReader) Close() error { return nil }
func TestErrorCaptureDoesNotReadUnboundedResponse(t *testing.T) {
	body := &diagnosticReader{}
	c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) { return response(500, body), nil }))
	_, err := c.Fetch(context.Background(), pageRequest())
	var f *ingest.Failure
	if !errors.As(err, &f) || body.n != diagnostic.CaptureBytes+1 || !f.Attempt.Response.Truncated {
		t.Fatal("error response read was not bounded")
	}
}

func TestBusinessFailureAlsoRetainsExplanation(t *testing.T) {
	c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		return response(200, io.NopCloser(strings.NewReader(`{"code":503,"message":"provider maintenance"}`))), nil
	}))
	_, err := c.Fetch(context.Background(), pageRequest())
	var f *ingest.Failure
	if !errors.As(err, &f) || f.Kind != ingest.BusinessFailure || f.Attempt.Response == nil || !strings.Contains(f.Attempt.Response.Body, "provider maintenance") {
		t.Fatal("missing business failure explanation")
	}
}
