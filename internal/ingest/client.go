package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Mode string

const (
	Full        Mode = "full"
	ForceFull   Mode = "force_full"
	Incremental Mode = "incremental"
)

type PageRequest struct {
	Table              string
	Mode               Mode
	Page, PageSize     int
	StartTime, EndTime string
}

type Attempt struct {
	Sent         bool
	HTTPStatus   *int
	BusinessCode *int64
}

type Page struct {
	Total   int64
	Records []Record
	Attempt Attempt
}

type FailureKind string

const (
	ConfigurationFailure FailureKind = "configuration"
	TransportFailure     FailureKind = "transport"
	HTTPFailure          FailureKind = "http"
	BusinessFailure      FailureKind = "business"
	StructureFailure     FailureKind = "structure"
	ResponseLimit        FailureKind = "response_limit"
	CancelledFailure     FailureKind = "cancelled"
	RecordFailure        FailureKind = "record"
)

// Failure never retains an upstream error, URL, response message or body.
// Cause is restricted to safe context sentinels or ValidationError.
type Failure struct {
	Kind      FailureKind
	Attempt   Attempt
	Cause     error
	retryable bool
}

func (e *Failure) Error() string    { return "source request: " + string(e.Kind) }
func (e *Failure) GoString() string { return e.Error() }
func (e *Failure) Unwrap() error    { return e.Cause }

type Client struct {
	origin   url.URL
	token    string
	http     *http.Client
	maxBytes int64
}

type closeErrorTransport struct{ next http.RoundTripper }

func (t closeErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(r)
	if err != nil && resp != nil {
		if resp.Body != nil {
			resp.Body.Close()
		}
		return nil, err
	}
	return resp, err
}

func (c *Client) String() string   { return "source client [redacted]" }
func (c *Client) GoString() string { return c.String() }

// NewClient requires an explicit origin and transport. Redirects are disabled:
// the upstream token must never follow a redirect to another request target.
func NewClient(origin, token string, transport http.RoundTripper, maxBytes int64) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		strings.TrimSpace(token) == "" || transport == nil || maxBytes < 1 || maxBytes > 64<<20 {
		return nil, &Failure{Kind: ConfigurationFailure}
	}
	return &Client{origin: *u, token: token, maxBytes: maxBytes, http: &http.Client{
		Transport:     closeErrorTransport{next: transport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) Fetch(ctx context.Context, input PageRequest) (Page, error) {
	a := Attempt{}
	fail := func(kind FailureKind, cause error) (Page, error) {
		return Page{}, &Failure{Kind: kind, Attempt: a, Cause: cause}
	}
	if err := ctx.Err(); err != nil {
		return fail(CancelledFailure, err)
	}
	d, ok := definitions[input.Table]
	if c == nil || !ok || input.Page < 1 || input.PageSize < 1 {
		return fail(ConfigurationFailure, nil)
	}
	q := url.Values{"access_token": {c.token}, "page": {strconv.Itoa(input.Page)}, "pageSize": {strconv.Itoa(input.PageSize)}}
	switch input.Mode {
	case Full, ForceFull:
	case Incremental:
		if input.StartTime == "" || input.EndTime == "" {
			return fail(ConfigurationFailure, nil)
		}
		q.Set("startTime", input.StartTime)
		q.Set("endTime", input.EndTime)
	default:
		return fail(ConfigurationFailure, nil)
	}
	u := c.origin
	u.Path, u.RawPath, u.RawQuery = d.Path, "", q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fail(ConfigurationFailure, nil)
	}
	req.Header.Set("Accept", "application/json")
	if err := ctx.Err(); err != nil {
		return fail(CancelledFailure, err)
	}
	a.Sent = true
	resp, err := c.http.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return fail(CancelledFailure, ctx.Err())
		}
		return Page{}, &Failure{Kind: TransportFailure, Attempt: a, retryable: temporaryTransport(err)}
	}
	if resp == nil {
		return fail(TransportFailure, nil)
	}
	status := resp.StatusCode
	a.HTTPStatus = &status
	if status < 200 || status >= 300 {
		return fail(HTTPFailure, nil)
	}
	if resp.Body == nil {
		return fail(StructureFailure, nil)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if ctx.Err() != nil {
		return fail(CancelledFailure, ctx.Err())
	}
	if err != nil {
		return Page{}, &Failure{Kind: TransportFailure, Attempt: a, retryable: temporaryTransport(err)}
	}
	if int64(len(body)) > c.maxBytes {
		return fail(ResponseLimit, nil)
	}
	envelope, err := objectFields(body)
	if err != nil {
		return fail(StructureFailure, nil)
	}
	code, valid := integer(envelope["code"])
	if !valid {
		return fail(BusinessFailure, nil)
	}
	a.BusinessCode = &code
	if code != 200 {
		return fail(BusinessFailure, nil)
	}
	// Source message is validated but never propagated to errors or telemetry.
	message := bytes.TrimSpace(envelope["message"])
	if len(message) == 0 || message[0] != '"' {
		return fail(StructureFailure, nil)
	}
	data, err := objectFields(envelope["data"])
	if err != nil {
		return fail(StructureFailure, nil)
	}
	total, valid := integer(data["total"])
	if !valid || total < 0 {
		return fail(StructureFailure, nil)
	}
	list := bytes.TrimSpace(data["list"])
	if len(list) == 0 || list[0] != '[' {
		return fail(StructureFailure, nil)
	}
	var rawRows []json.RawMessage
	if err := json.Unmarshal(list, &rawRows); err != nil {
		return fail(StructureFailure, nil)
	}
	if int64(len(rawRows)) > total || len(rawRows) > input.PageSize {
		return fail(StructureFailure, nil)
	}
	rows := make([]Record, 0, len(rawRows))
	for _, raw := range rawRows {
		if ctx.Err() != nil {
			return fail(CancelledFailure, ctx.Err())
		}
		record, err := DecodeRecord(input.Table, raw)
		if err != nil {
			return fail(RecordFailure, err)
		}
		rows = append(rows, record)
	}
	return Page{Total: total, Records: rows, Attempt: a}, nil
}

// Decode objects explicitly so duplicate envelope keys cannot silently win.
func objectFields(raw []byte) (map[string]json.RawMessage, error) {
	invalid := errors.New("invalid object")
	if !utf8.Valid(raw) {
		return nil, invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return nil, invalid
	}
	result := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, invalid
		}
		name, ok := key.(string)
		if !ok {
			return nil, invalid
		}
		if _, exists := result[name]; exists {
			return nil, invalid
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, invalid
		}
		result[name] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return nil, invalid
	}
	var extra json.RawMessage
	if d.Decode(&extra) != io.EOF {
		return nil, invalid
	}
	return result, nil
}

func integer(raw []byte) (int64, bool) {
	s := string(bytes.TrimSpace(raw))
	if s == "" {
		return 0, false
	}
	for i, ch := range s {
		if ch == '-' && i == 0 {
			continue
		}
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}
