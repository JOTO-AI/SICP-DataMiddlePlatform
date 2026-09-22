package ingest_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

const emptyResponse = `{"code":200,"message":"ok","data":{"total":0,"list":[]}}`

func sourceClient(t *testing.T, transport http.RoundTripper) *ingest.Client {
	t.Helper()
	c, err := ingest.NewClient("https://mock.invalid", "fictional-secret", transport, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func pageRequest() ingest.PageRequest {
	return ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: ingest.Full, Page: 1, PageSize: 2}
}
func response(status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: body}
}

func TestT03_06FullModesOmitTimeOnEveryRequest(t *testing.T) {
	for _, mode := range []ingest.Mode{ingest.Full, ingest.ForceFull} {
		calls := 0
		c := sourceClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			q := r.URL.Query()
			if r.Method != "GET" || r.URL.Path != "/apizt/openapi/invoke/sjzc/release/dwd_xqjbsjzlb" ||
				q.Get("access_token") != "fictional-secret" || q.Get("page") != fmt.Sprint(calls) || q.Get("pageSize") != "2" ||
				q.Has("startTime") || q.Has("endTime") || len(q) != 3 {
				t.Fatal("unexpected request")
			}
			return response(200, io.NopCloser(strings.NewReader(emptyResponse))), nil
		}))
		for page := 1; page <= 2; page++ {
			r := pageRequest()
			r.Mode = mode
			r.Page = page
			r.StartTime = "ignored"
			r.EndTime = "ignored"
			if _, err := c.Fetch(context.Background(), r); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestT03_07IncrementalKeepsFixedWindow(t *testing.T) {
	c := sourceClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if q.Get("startTime") != "2026-08-05 07:55:00" || q.Get("endTime") != "2026-08-05 10:00:00" {
			t.Fatal("window changed")
		}
		return response(200, io.NopCloser(strings.NewReader(emptyResponse))), nil
	}))
	r := pageRequest()
	r.Mode = ingest.Incremental
	r.StartTime = "2026-08-05 07:55:00"
	r.EndTime = "2026-08-05 10:00:00"
	for _, page := range []int{1, 2, 2} {
		r.Page = page
		if _, err := c.Fetch(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestT03_08HTTPAndBusinessFailuresAreSeparate(t *testing.T) {
	for _, tc := range []struct {
		status   int
		code     string
		kind     ingest.FailureKind
		business bool
	}{
		{401, "200", ingest.HTTPFailure, false}, {503, "200", ingest.HTTPFailure, false},
		{200, "401", ingest.BusinessFailure, true}, {200, `"200"`, ingest.BusinessFailure, false},
		{200, "200.0", ingest.BusinessFailure, false}, {200, "2e2", ingest.BusinessFailure, false}, {200, "null", ingest.BusinessFailure, false},
	} {
		t.Run(fmt.Sprintf("%d_%s", tc.status, tc.code), func(t *testing.T) {
			b := &trackedBody{Reader: strings.NewReader(strings.Replace(emptyResponse, "200", tc.code, 1))}
			c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) { return response(tc.status, b), nil }))
			p, err := c.Fetch(context.Background(), pageRequest())
			var failure *ingest.Failure
			if !errors.As(err, &failure) || failure.Kind != tc.kind || !failure.Attempt.Sent || failure.Attempt.HTTPStatus == nil ||
				*failure.Attempt.HTTPStatus != tc.status || (failure.Attempt.BusinessCode != nil) != tc.business || p.Records != nil || !b.closed {
				t.Fatalf("bad failure: %#v", err)
			}
		})
	}
}

func TestT03_09InvalidEnvelopesNeverBecomeEmptyPages(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `[]`, `{"code":200,"message":"ok"}`, `{"code":200,"message":"ok","data":null}`,
		`{"code":200,"message":"ok","data":[]}`, `{"code":200,"message":null,"data":{"total":0,"list":[]}}`,
		`{"code":200,"message":"ok","data":{"total":0}}`, `{"code":200,"message":"ok","data":{"total":0,"list":null}}`,
		`{"code":200,"message":"ok","data":{"total":0,"list":{}}}`, `{"code":200,"message":"ok","data":{"list":[]}}`,
		`{"code":200,"message":"ok","data":{"total":-1,"list":[]}}`, `{"code":200,"message":"ok","data":{"total":0.0,"list":[]}}`,
		`{"code":200,"message":"ok","data":{"total":"0","list":[]}}`, `{"code":200,"message":"ok","data":{"total":9223372036854775808,"list":[]}}`,
		`{"code":200,"code":200,"message":"ok","data":{"total":0,"list":[]}}`, emptyResponse + `{}`,
		`{"code":200,"message":"ok","data":{"total":0,"total":0,"list":[]}}`,
		`{"code":200,"message":"ok","data":{"total":0,"list":[{"XQH":"01"}]}}`,
	} {
		b := &trackedBody{Reader: strings.NewReader(body)}
		c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) { return response(200, b), nil }))
		p, err := c.Fetch(context.Background(), pageRequest())
		if err == nil || p.Records != nil || !b.closed {
			t.Fatal("invalid envelope accepted or body leaked")
		}
	}
}

func TestT03_09ValidRecordsRemainLosslessAndFailuresDiscardWholePage(t *testing.T) {
	for _, valid := range []bool{true, false} {
		rows := `{"XQH":"01"}`
		if !valid {
			rows += `,{"XQH":null}`
		}
		b := &trackedBody{Reader: strings.NewReader(`{"code":200,"message":"ok","data":{"total":2,"list":[` + rows + `]}}`)}
		c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) { return response(200, b), nil }))
		p, err := c.Fetch(context.Background(), pageRequest())
		if valid {
			if err != nil || len(p.Records) != 1 || p.Records[0].Key != "01" || p.Total != 2 || !p.Attempt.Sent {
				t.Fatalf("bad page: %v", err)
			}
		} else if err == nil || p.Records != nil {
			t.Fatal("partial page escaped")
		}
		if !b.closed {
			t.Fatal("body leaked")
		}
	}
}

func TestT03_24CancelledBeforeSendHasNoTransportCall(t *testing.T) {
	c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("request sent"); return nil, nil }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Fetch(ctx, pageRequest())
	var f *ingest.Failure
	if !errors.As(err, &f) || f.Attempt.Sent || f.Attempt.HTTPStatus != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation metadata")
	}
}

type readError struct{}

func (readError) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestT03_25ErrorsAreRedactedAndBodiesAlwaysClose(t *testing.T) {
	for _, scenario := range []string{"success", "http", "parse", "read", "transport", "limit", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := emptyResponse
			if scenario == "parse" {
				body = `{"fictional-secret"`
			}
			if scenario == "limit" {
				body = strings.Repeat(" ", 1<<20+1)
			}
			b := &trackedBody{Reader: strings.NewReader(body)}
			if scenario == "read" {
				b.Reader = readError{}
			}
			c := sourceClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				if scenario == "cancel" {
					cancel()
				}
				if scenario == "transport" {
					return response(200, b), &url.Error{Op: "Get", URL: r.URL.String(), Err: errors.New("fictional-secret")}
				}
				status := 200
				if scenario == "http" {
					status = 403
				}
				return response(status, b), nil
			}))
			_, err := c.Fetch(ctx, pageRequest())
			if (err == nil) != (scenario == "success") || !b.closed {
				t.Fatal("wrong result or leaked body")
			}
			for e := err; e != nil; e = errors.Unwrap(e) {
				if strings.Contains(fmt.Sprintf("%v %+v %#v", e, e, e), "fictional-secret") {
					t.Fatal("secret leaked")
				}
			}
			if strings.Contains(fmt.Sprintf("%v %#v", c, c), "fictional-secret") {
				t.Fatal("client formatting leaked")
			}
		})
	}
}

func TestT03_25RedirectCannotForwardToken(t *testing.T) {
	calls := 0
	b := &trackedBody{Reader: strings.NewReader("")}
	c := sourceClient(t, transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		r := response(302, b)
		r.Header.Set("Location", "https://another.invalid")
		return r, nil
	}))
	_, err := c.Fetch(context.Background(), pageRequest())
	if err == nil || calls != 1 || !b.closed {
		t.Fatal("redirect followed or body leaked")
	}
}

func TestT03_09InvalidConfigurationSendsNothing(t *testing.T) {
	rt := transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid request sent"); return nil, nil })
	for _, origin := range []string{"", "ftp://mock.invalid", "https://user:pass@mock.invalid", "https://mock.invalid/path", "https://mock.invalid?access_token=secret", "https://mock.invalid#fragment"} {
		if _, err := ingest.NewClient(origin, "fictional", rt, 100); err == nil {
			t.Fatal("bad origin accepted")
		}
	}
	c := sourceClient(t, rt)
	for _, alter := range []func(*ingest.PageRequest){
		func(r *ingest.PageRequest) { r.Table = "other" }, func(r *ingest.PageRequest) { r.Mode = "other" },
		func(r *ingest.PageRequest) { r.Page = 0 }, func(r *ingest.PageRequest) { r.PageSize = 0 }, func(r *ingest.PageRequest) { r.Mode = ingest.Incremental },
	} {
		r := pageRequest()
		alter(&r)
		if _, err := c.Fetch(context.Background(), r); err == nil {
			t.Fatal("bad request accepted")
		}
	}
}
