package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const tokenTestPage = `{"code":200,"message":"ok","data":{"total":0,"list":[]}}`

func tokenPageRequest() PageRequest {
	return PageRequest{Table: "dwd_xqjbsjzlb", Mode: Incremental, Page: 2, PageSize: 10, StartTime: "2026-09-01 00:00:00", EndTime: "2026-09-02 00:00:00"}
}

func authenticatedTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c, err := NewAuthenticatedClient(s.URL, "test-app-id", "test-secret-@:/?#%$", s.Client().Transport, 1<<20, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTokenAcquisitionCacheAndEarlyRefresh(t *testing.T) {
	var issued atomic.Int32
	c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path == "/apizt/opentoken/oauth/token" {
			if r.Method != "GET" || len(q) != 2 || q.Get("appId") != "test-app-id" || q.Get("secret") != "test-secret-@:/?#%$" {
				t.Error("incorrect credential encoding or auth contract")
			}
			fmt.Fprintf(w, `{"code":200,"data":{"access_token":"token-%d","expires_in":"100"}}`, issued.Add(1))
			return
		}
		if q.Get("access_token") != fmt.Sprintf("token-%d", issued.Load()) || q.Has("secret") || q.Has("appId") ||
			q.Get("page") != "2" || q.Get("pageSize") != "10" || q.Get("startTime") != tokenPageRequest().StartTime || q.Get("endTime") != tokenPageRequest().EndTime {
			t.Error("page token or pagination/window changed")
		}
		io.WriteString(w, tokenTestPage)
	})
	now := time.Now()
	c.auth.now = func() time.Time { return now }
	for _, elapsed := range []time.Duration{0, 89 * time.Second, time.Second} {
		now = now.Add(elapsed)
		if _, err := c.Fetch(t.Context(), tokenPageRequest()); err != nil {
			t.Fatal(err)
		}
	}
	if issued.Load() != 2 {
		t.Fatal("token was not reused then refreshed before expiration", issued.Load())
	}
}

func TestUnknownTokenLifetimeSharesTokenUntilExplicitRejection(t *testing.T) {
	var issued atomic.Int32
	c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth/token") {
			fmt.Fprintf(w, `{"code":200,"data":{"access_token":"token-%d"}}`, issued.Add(1))
			return
		}
		if r.URL.Query().Get("access_token") != fmt.Sprintf("token-%d", issued.Load()) {
			t.Error("a new token invalidated an in-flight page")
		}
		io.WriteString(w, tokenTestPage)
	})
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := c.Fetch(t.Context(), tokenPageRequest()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if issued.Load() != 1 {
		t.Fatal("concurrent pages should share one token")
	}
}

func TestAuthenticatedPageRequestsActuallyOverlap(t *testing.T) {
	for _, expiry := range []string{"", `,"expires_in":3600`} {
		t.Run(expiry, func(t *testing.T) {
			var issued, active atomic.Int32
			gate := make(chan struct{})
			c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth/token") {
					issued.Add(1)
					fmt.Fprintf(w, `{"code":200,"data":{"access_token":"shared"%s}}`, expiry)
					return
				}
				if active.Add(1) == 3 {
					close(gate)
				}
				select {
				case <-gate:
					io.WriteString(w, tokenTestPage)
				case <-r.Context().Done():
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var wg sync.WaitGroup
			for range 3 {
				wg.Go(func() {
					if _, err := c.Fetch(ctx, tokenPageRequest()); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			if active.Load() != 3 || issued.Load() != 1 {
				t.Fatal("authentication serialized or issued competing tokens", active.Load(), issued.Load())
			}
		})
	}
}

func TestRejectedTokenRefreshWaitsForOldPagesAndAllowsWaiterCancellation(t *testing.T) {
	var issued, oldRequests atomic.Int32
	bothOld := make(chan struct{})
	releaseOld := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseOld) }) }
	c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth/token") {
			fmt.Fprintf(w, `{"code":200,"data":{"access_token":"token-%d"}}`, issued.Add(1))
			return
		}
		if r.URL.Query().Get("access_token") == "token-1" {
			if oldRequests.Add(1) == 2 {
				close(bothOld)
			}
			select {
			case <-bothOld:
			case <-r.Context().Done():
				return
			}
			if r.URL.Query().Get("page") == "1" {
				w.WriteHeader(401)
				return
			}
			select {
			case <-releaseOld:
			case <-r.Context().Done():
				return
			}
			if issued.Load() != 1 {
				t.Error("refresh invalidated a page still using the old token")
			}
		}
		io.WriteString(w, tokenTestPage)
	})
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan error, 2)
	for page := 1; page <= 2; page++ {
		go func() { r := tokenPageRequest(); r.Page = page; _, err := c.Fetch(ctx, r); done <- err }()
	}
	for {
		c.auth.gate <- struct{}{}
		draining := c.auth.token == "" && c.auth.active == 1
		<-c.auth.gate
		if draining {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("did not wait for old token leases")
		case <-time.After(time.Millisecond):
		}
	}
	wait, cancelWait := context.WithCancel(ctx)
	cancelWait()
	if _, err := c.leaseToken(wait); !errors.Is(err, context.Canceled) {
		t.Fatal("token waiter ignored cancellation", err)
	}
	if issued.Load() != 1 {
		t.Fatal("refreshed before draining old requests")
	}
	release()
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if issued.Load() != 2 {
		t.Fatal("refresh did not issue exactly one replacement", issued.Load())
	}
}

func TestTokenRejectionRetriesCurrentPageOnlyOnce(t *testing.T) {
	for _, rejection := range []string{"http401", "http403", "business401", "business403"} {
		for _, permanent := range []bool{false, true} {
			t.Run(fmt.Sprint(rejection, permanent), func(t *testing.T) {
				var issued, pages atomic.Int32
				c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/oauth/token") {
						fmt.Fprintf(w, `{"code":200,"data":{"access_token":"token-%d","expires_in":3600}}`, issued.Add(1))
						return
					}
					if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("startTime") != tokenPageRequest().StartTime {
						t.Error("retry changed source page")
					}
					if pages.Add(1) == 1 || permanent {
						switch rejection {
						case "http401":
							w.WriteHeader(401)
						case "http403":
							w.WriteHeader(403)
						case "business401":
							io.WriteString(w, `{"code":401}`)
						case "business403":
							io.WriteString(w, `{"code":403}`)
						}
						return
					}
					io.WriteString(w, tokenTestPage)
				})
				_, err := c.Fetch(t.Context(), tokenPageRequest())
				if (err != nil) != permanent || issued.Load() != 2 || pages.Load() != 2 {
					t.Fatal("incorrect auth retry bound", err, issued.Load(), pages.Load())
				}
			})
		}
	}
}

func TestTokenDoesNotRetryUnrelatedBusinessErrors(t *testing.T) {
	var calls atomic.Int32
	c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.HasSuffix(r.URL.Path, "/oauth/token") {
			io.WriteString(w, `{"code":200,"data":{"access_token":"temporary-token"}}`)
			return
		}
		io.WriteString(w, `{"code":500,"message":"invalid data"}`)
	})
	_, err := c.Fetch(t.Context(), tokenPageRequest())
	if err == nil || calls.Load() != 2 {
		t.Fatal("unrelated failure triggered auth refresh")
	}
}

func TestInvalidAuthenticationNeverFetchesDataOrLeaks(t *testing.T) {
	for _, body := range []string{
		`{"code":403,"message":"test-secret-@:/?#%$"}`,
		`{"code":200,"data":{"access_token":""}}`,
		`{"code":200,"data":{"access_token":null}}`,
		`{"code":200,"data":{"access_token":42}}`,
		`{"code":200,"data":{"access_token":"one","access_token":"two"}}`,
		`{"code":200,"data":{"access_token":"token"}} trailing`,
		strings.Repeat("x", 65537),
	} {
		c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/oauth/token") {
				t.Error("data requested after failed authentication")
			}
			io.WriteString(w, body)
		})
		_, err := c.Fetch(t.Context(), tokenPageRequest())
		var f *Failure
		if !errors.As(err, &f) || f.Kind != AuthenticationFailure || f.Attempt.Sent {
			t.Fatal("expected safe authentication failure", err)
		}
		formatted := fmt.Sprintf("%+v %#v %+v %#v", err, err, c, c.auth)
		for _, secret := range []string{"test-secret", "test-app-id", body} {
			if strings.Contains(formatted, secret) {
				t.Fatal("authentication leaked")
			}
		}
	}
}

func TestTokenFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		status             int
	}{
		{"gateway", "private upstream response", "http", 502},
		{"business", `{"code":403,"message":"private"}`, "business", 200},
		{"structure", "not JSON", "structure", 200},
		{"token", `{"code":200,"data":{"access_token":""}}`, "invalid_token", 200},
		{"limit", strings.Repeat("x", 65537), "response_limit", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/oauth/token") {
					t.Error("data page requested")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			_, err := c.Fetch(t.Context(), tokenPageRequest())
			var f *Failure
			if !errors.As(err, &f) || f.Attempt.Authentication == nil {
				t.Fatal("missing auth diagnostic", err)
			}
			a := f.Attempt.Authentication
			if !a.Valid() || a.Reason != tc.reason || a.HTTPStatus != tc.status || f.Attempt.HTTPStatus != nil || f.Attempt.Sent {
				t.Fatal("incorrect token failure classification")
			}
		})
	}
}

func TestAuthenticationRedirectAndTimeout(t *testing.T) {
	var unexpected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { unexpected.Add(1) }))
	defer other.Close()
	c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) })
	if _, err := c.Fetch(t.Context(), tokenPageRequest()); err == nil || unexpected.Load() != 0 {
		t.Fatal("authentication followed redirect")
	}
	c = authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	c.auth.timeout = 30 * time.Millisecond
	_, err := c.Fetch(t.Context(), tokenPageRequest())
	var failure *Failure
	if !errors.As(err, &failure) || failure.Attempt.Authentication == nil || failure.Attempt.Authentication.Reason != "timeout" || failure.Attempt.Authentication.HTTPStatus != 200 {
		t.Fatal("authentication body timeout diagnostic missing")
	}
}

func TestAuthenticationGateCancellationAndInvalidInput(t *testing.T) {
	c := authenticatedTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") })
	if _, err := c.Fetch(t.Context(), PageRequest{}); err == nil {
		t.Fatal("invalid input accepted")
	}
	c.auth.gate <- struct{}{}
	defer func() { <-c.auth.gate }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Fetch(ctx, tokenPageRequest()); !errors.Is(err, context.Canceled) {
		t.Fatal("gate ignored cancellation", err)
	}
}

func TestFailedTokenRefreshPreservesSourceAttempt(t *testing.T) {
	var auth atomic.Int32
	c := authenticatedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth/token") {
			if auth.Add(1) == 1 {
				io.WriteString(w, `{"code":200,"data":{"access_token":"first-token"}}`)
			} else {
				w.WriteHeader(503)
			}
			return
		}
		w.WriteHeader(401)
	})
	_, err := c.Fetch(t.Context(), tokenPageRequest())
	var f *Failure
	if !errors.As(err, &f) || f.Kind != AuthenticationFailure || !f.Attempt.Sent || f.Attempt.HTTPStatus == nil || *f.Attempt.HTTPStatus != 401 {
		t.Fatal("refresh failure lost the rejected page attempt", err)
	}
	if f.Attempt.Authentication == nil || f.Attempt.Authentication.HTTPStatus != 503 || f.Attempt.Authentication.Reason != "http" {
		t.Fatal("refresh failure lost token endpoint evidence")
	}
}

func TestTokenLifetimeParsing(t *testing.T) {
	for _, raw := range []string{"", "null", "0", "-1", "1.5", "true", `"tomorrow"`, "9223372036854775807"} {
		if _, ok := expirySeconds([]byte(raw)); ok {
			t.Fatalf("invalid lifetime accepted: %s", raw)
		}
	}
	for _, raw := range []string{"3600", `"3600"`} {
		if n, ok := expirySeconds([]byte(raw)); !ok || n != 3600 {
			t.Fatal("valid lifetime rejected")
		}
	}
}
