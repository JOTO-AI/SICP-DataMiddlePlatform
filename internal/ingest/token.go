package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const AuthenticationFailure FailureKind = "authentication"

type sourceAuth struct {
	appID, secret string
	token         string
	validUntil    time.Time
	timeout       time.Duration
	// Protect token state and issuance, while allowing concurrent page leases.
	// Refresh drains old leases first because issuance may invalidate old tokens.
	gate    chan struct{}
	changed chan struct{}
	active  int
	now     func() time.Time
}

func (*sourceAuth) String() string     { return "source authentication [redacted]" }
func (a *sourceAuth) GoString() string { return a.String() }

// NewAuthenticatedClient uses the same-origin endpoint verified by the source
// audit script. It never obtains credentials from captured documentation.
func NewAuthenticatedClient(origin, appID, secret string, transport http.RoundTripper, maxBytes int64, timeout time.Duration) (*Client, error) {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(secret) == "" || timeout <= 0 || timeout > time.Minute {
		return nil, &Failure{Kind: ConfigurationFailure}
	}
	c, err := NewClient(origin, "unused", transport, maxBytes)
	if err != nil {
		return nil, err
	}
	c.token = ""
	c.auth = &sourceAuth{appID: appID, secret: secret, timeout: timeout, gate: make(chan struct{}, 1), changed: make(chan struct{}), now: time.Now}
	return c, nil
}

func (c *Client) fetchAuthenticated(ctx context.Context, input PageRequest) (Page, error) {
	if _, ok := definitions[input.Table]; !ok || input.Page < 1 || input.PageSize < 1 ||
		(input.Mode != Full && input.Mode != ForceFull && input.Mode != Incremental) ||
		(input.Mode == Incremental && (input.StartTime == "" || input.EndTime == "")) {
		return Page{}, &Failure{Kind: ConfigurationFailure}
	}
	var previous Attempt
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return Page{}, &Failure{Kind: CancelledFailure, Cause: err, Attempt: previous}
		}
		token, err := c.leaseToken(ctx)
		if err != nil {
			var f *Failure
			if errors.As(err, &f) {
				f.Attempt = previous
			}
			return Page{}, err
		}
		page, err := c.fetch(ctx, input, token)
		rejected := authenticationRejected(err)
		c.releaseToken(token, rejected)
		if !rejected {
			return page, err
		}
		var f *Failure
		if errors.As(err, &f) {
			previous = f.Attempt
		}
		if attempt == 1 {
			return page, err
		}
	}
	panic("unreachable authentication retry")
}

func (c *Client) leaseToken(ctx context.Context) (string, error) {
	a := c.auth
	for {
		select {
		case a.gate <- struct{}{}:
		case <-ctx.Done():
			return "", &Failure{Kind: CancelledFailure, Cause: ctx.Err()}
		}
		if a.token != "" && (a.validUntil.IsZero() || a.now().Before(a.validUntil)) {
			a.active++
			token := a.token
			<-a.gate
			return token, nil
		}
		if a.active > 0 {
			changed := a.changed
			<-a.gate
			select {
			case <-changed:
				continue
			case <-ctx.Done():
				return "", &Failure{Kind: CancelledFailure, Cause: ctx.Err()}
			}
		}
		err := c.acquireToken(ctx)
		token := a.token
		if err == nil {
			a.active++
		}
		<-a.gate
		return token, err
	}
}

func (c *Client) releaseToken(token string, rejected bool) {
	a := c.auth
	a.gate <- struct{}{}
	if rejected && a.token == token {
		a.token, a.validUntil = "", time.Time{}
	}
	a.active--
	close(a.changed)
	a.changed = make(chan struct{})
	<-a.gate
}

// Only explicit authorization status codes trigger the single retry. Do not
// guess vendor-specific codes from response messages or retry business errors.
func authenticationRejected(err error) bool {
	var f *Failure
	if !errors.As(err, &f) {
		return false
	}
	if f.Kind == HTTPFailure && f.Attempt.HTTPStatus != nil {
		return *f.Attempt.HTTPStatus == 401 || *f.Attempt.HTTPStatus == 403
	}
	return f.Kind == BusinessFailure && f.Attempt.BusinessCode != nil &&
		(*f.Attempt.BusinessCode == 401 || *f.Attempt.BusinessCode == 403)
}

func (c *Client) acquireToken(parent context.Context) error {
	a := c.auth
	a.token, a.validUntil = "", time.Time{}
	ctx, cancel := context.WithTimeout(parent, a.timeout)
	defer cancel()
	// Count validity from before the request, not from response arrival.
	started := a.now()
	u := c.origin
	u.Path, u.RawPath = "/apizt/opentoken/oauth/token", ""
	u.RawQuery = url.Values{"appId": {a.appID}, "secret": {a.secret}}.Encode()
	fail := func() error {
		if parent.Err() != nil {
			return &Failure{Kind: CancelledFailure, Cause: parent.Err()}
		}
		// Do not retain auth response data, status metadata or URL-bearing errors
		// as a source-page attempt. No data page was sent.
		return &Failure{Kind: AuthenticationFailure}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fail()
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK || resp.Body == nil {
		return fail()
	}
	const limit = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(body) > limit || ctx.Err() != nil {
		return fail()
	}
	envelope, err := objectFields(body)
	if err != nil {
		return fail()
	}
	if code, ok := integer(envelope["code"]); !ok || code != 200 {
		return fail()
	}
	data, err := objectFields(envelope["data"])
	if err != nil {
		return fail()
	}
	var token string
	if json.Unmarshal(data["access_token"], &token) != nil || strings.TrimSpace(token) == "" || len(token) > 16384 {
		return fail()
	}
	// The captured contract does not specify a lifetime. Recognize an optional
	// expires_in in seconds. Without a usable expiry, reuse the in-memory token
	// until explicit rejection; do not invent a TTL or issue per-page tokens
	// that could invalidate concurrent requests. Never persist tokens to disk.
	if seconds, ok := expirySeconds(data["expires_in"]); ok {
		ttl := time.Duration(seconds) * time.Second
		lead := min(ttl/10, 30*time.Second)
		a.validUntil = started.Add(ttl - lead)
		if !a.now().Before(a.validUntil) {
			return fail()
		}
	}
	a.token = token
	return nil
}

func expirySeconds(raw json.RawMessage) (int64, bool) {
	var quoted string
	if json.Unmarshal(raw, &quoted) == nil {
		raw = json.RawMessage(quoted)
	}
	n, ok := integer(raw)
	return n, ok && n > 0 && n <= int64((1<<63-1)/time.Second)
}
