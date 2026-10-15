package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
)

// requestEvidence lives only for one request. Raw response bytes and transport
// errors are discarded after the failure is reduced to bounded safe evidence.
type requestEvidence struct {
	client    *Client
	request   diagnostic.Request
	secrets   []string
	header    http.Header
	body      []byte
	readError bool
	truncated bool
	err       error
	message   string
}

func (c *Client) evidence(kind, path, token string) *requestEvidence {
	if path == "" {
		path = "/"
	}
	endpoint := path
	secrets := []string{token}
	if c != nil {
		u := c.origin
		u.Path, u.RawPath, u.RawQuery, u.Fragment = path, "", "", ""
		endpoint = u.String()
		secrets = append(secrets, c.token)
		if c.auth != nil {
			secrets = append(secrets, c.auth.appID, c.auth.secret)
		}
	}
	return &requestEvidence{client: c, secrets: secrets, request: diagnostic.Request{Kind: kind, Endpoint: endpoint, Method: "GET", StartedAt: time.Now().UTC(), Stage: "prepare"}}
}

func (e *requestEvidence) send(req *http.Request) (*http.Response, error) {
	e.request.Stage, e.request.Sent = "transport", true
	resp, err := e.client.http.Do(req)
	e.err = err
	if resp != nil {
		e.request.HTTPStatus, e.header = resp.StatusCode, resp.Header
	}
	return resp, err
}

func (e *requestEvidence) read(body io.Reader, limit int64) ([]byte, error) {
	e.request.Stage = "read"
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	e.body, e.err, e.readError, e.truncated = data, err, err != nil, int64(len(data)) > limit
	return data, err
}

func (e *requestEvidence) finish(ctx context.Context, failure *Failure) {
	if failure == nil {
		return
	}
	r := e.request
	r.DurationNS = max(0, time.Since(r.StartedAt).Nanoseconds())
	r.Failure = string(failure.Kind)
	if a := failure.Attempt.Authentication; a != nil {
		r.Failure = a.Reason
	}
	if e.err == nil && ctx.Err() != nil {
		e.err = ctx.Err()
	}
	if e.err == nil && failure.Cause != nil {
		e.err = failure.Cause
	}
	r.Error, r.ErrorType, r.ErrorTruncated = diagnostic.ErrorText(e.err, e.secrets...)
	if r.Error == "" {
		r.Error = e.message
	}
	if r.Error == "" {
		r.Error = "source request failed during " + r.Stage + ": " + r.Failure
	}
	if r.HTTPStatus != 0 {
		r.Response = *diagnostic.Capture(e.header, e.body, e.readError, e.secrets...)
		// A token-success payload can itself contain credentials, even if it
		// subsequently fails validation. Keep headers and local validation
		// details, never a preview of a purported successful token response.
		if r.Kind == "authentication" && r.BusinessCode == "200" {
			r.Response = *diagnostic.Capture(e.header, nil, e.readError, e.secrets...)
			r.Response.Unavailable = "success_payload_omitted"
		}
		r.Response.Truncated = r.Response.Truncated || e.truncated
		if r.BusinessCode == "" && len(e.body) <= diagnostic.CaptureBytes {
			var object map[string]json.RawMessage
			if json.Unmarshal(e.body, &object) == nil {
				if code, ok := integer(object["code"]); ok {
					r.BusinessCode = strconv.FormatInt(code, 10)
				} else {
					var code string
					if json.Unmarshal(object["code"], &code) == nil {
						r.BusinessCode = code
					}
				}
			}
		}
	}
	if r.Kind == "data" && failure.Attempt.BusinessCode != nil {
		r.BusinessCode = strconv.FormatInt(*failure.Attempt.BusinessCode, 10)
	}
	r = r.Sanitize(e.secrets...)
	failure.Attempt.Diagnostics = append(failure.Attempt.Diagnostics, r)
}

// Covers admission/cooldown failures and unforeseen errors outside HTTP too.
// Existing transport/parse evidence always wins over this local fallback.
func ensureEvidence(input PageRequest, err error) *Failure {
	var f *Failure
	if !errors.As(err, &f) {
		f = &Failure{Kind: FailureKind("unknown")}
	}
	if len(f.Attempt.Diagnostics) != 0 {
		return f
	}
	if f.Attempt.Completed != nil {
		r := *f.Attempt.Completed
		r.Stage, r.Failure = "schedule", string(f.Kind)
		r.Error, r.ErrorType, r.ErrorTruncated = diagnostic.ErrorText(err)
		f.Attempt.Diagnostics = []diagnostic.Request{r.Sanitize()}
		return f
	}
	e := (*Client)(nil).evidence("data", definitions[input.Table].Path, "")
	e.request.Stage = "schedule"
	e.request.Sent = f.Attempt.Sent
	if f.Attempt.HTTPStatus != nil {
		e.request.HTTPStatus = *f.Attempt.HTTPStatus
	}
	e.err = err
	e.finish(context.Background(), f)
	return f
}
