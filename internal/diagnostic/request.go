package diagnostic

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Request is the common evidence contract for every failed upstream exchange.
// Classification is descriptive, never an allowlist of provider failures.
// Values (rather than pointers) keep copies immutable across event retries.
type Request struct {
	Kind           string    `json:"kind"`
	Method         string    `json:"method"`
	Endpoint       string    `json:"endpoint"`
	StartedAt      time.Time `json:"started_at"`
	DurationNS     int64     `json:"duration_ns"`
	Sent           bool      `json:"sent"`
	HTTPStatus     int       `json:"http_status,omitempty"`
	BusinessCode   string    `json:"business_code,omitempty"`
	Stage          string    `json:"stage"`
	Failure        string    `json:"failure"`
	Error          string    `json:"error,omitempty"`
	ErrorType      string    `json:"error_type,omitempty"`
	ErrorTruncated bool      `json:"error_truncated,omitempty"`
	Response       Response  `json:"response,omitzero"`
}

func (Request) String() string     { return "[upstream request diagnostic redacted]" }
func (r Request) GoString() string { return r.String() }

// URL errors contain the original credential-bearing request URL. Unwrap that
// wrapper, and strip any URLs embedded in the underlying error as well.
var errorURL = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)

func ErrorText(err error, secrets ...string) (string, string, bool) {
	if err == nil {
		return "", "", false
	}
	var u *url.Error
	if errors.As(err, &u) && u.Err != nil {
		err = u.Err
	}
	message := errorURL.ReplaceAllString(err.Error(), "[request URL omitted]")
	message, clipped := clip(Redact(message, secrets...), 512)
	kind, _ := clip(fmt.Sprintf("%T", err), 128)
	return message, kind, clipped
}

func (r Request) Valid() bool {
	if r.Kind != "data" && r.Kind != "authentication" {
		return false
	}
	if r.Method != "GET" || r.StartedAt.IsZero() || r.DurationNS < 0 || (r.HTTPStatus != 0 && (!r.Sent || r.HTTPStatus < 100 || r.HTTPStatus > 599)) {
		return false
	}
	u, err := url.Parse(r.Endpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if !strings.HasPrefix(u.Path, "/") || len(r.Endpoint) > 512 {
		return false
	}
	for _, v := range []string{r.Kind, r.Method, r.Endpoint, r.Stage, r.Failure, r.Error, r.ErrorType, r.BusinessCode} {
		if !utf8.ValidString(v) {
			return false
		}
	}
	return len(r.Stage) > 0 && len(r.Stage) <= 64 && len(r.Failure) > 0 && len(r.Failure) <= 64 && len(r.Error) <= 512 && len(r.ErrorType) <= 128 && len(r.BusinessCode) <= 128 && r.Response.Valid()
}

func (r Request) Sanitize(secrets ...string) Request {
	r.Endpoint, _ = clip(Redact(r.Endpoint, secrets...), 512)
	r.Stage, _ = clip(Redact(r.Stage, secrets...), 64)
	r.Failure, _ = clip(Redact(r.Failure, secrets...), 64)
	r.Error, _ = clip(Redact(r.Error, secrets...), 512)
	r.ErrorType, _ = clip(Redact(r.ErrorType, secrets...), 128)
	r.BusinessCode, _ = clip(Redact(r.BusinessCode, secrets...), 128)
	r.Response = r.Response.Sanitize(secrets...)
	return r
}

// LimitText allows the event boundary to keep request identities and statuses
// even when combined evidence would exceed its configured payload budget.
func (r Request) LimitText(n int) Request {
	var clipped bool
	r.Response.Body, clipped = clip(r.Response.Body, n)
	r.Response.Truncated = r.Response.Truncated || clipped
	r.Error, clipped = clip(r.Error, min(n, 512))
	r.ErrorTruncated = r.ErrorTruncated || clipped
	return r
}
