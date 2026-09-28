package admin

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

// QueryProxy only forwards the four public query operations and health checks.
// The browser never receives the service API key or controls the upstream host.
func QueryProxy(address, apiKey string) (http.Handler, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") || apiKey == "" {
		return nil, errors.New("invalid console query endpoint")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 30 * time.Second
	proxy := &httputil.ReverseProxy{Transport: transport, Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(u)
		p.Out.Host = u.Host
		p.Out.Header = make(http.Header)
		p.Out.Header.Set("X-API-Key", apiKey)
		p.Out.Header.Set("Content-Type", "application/json")
		p.Out.Header.Set("Accept", "application/json")
		if snapshot := p.In.Header.Get("X-Query-Snapshot"); snapshot != "" {
			p.Out.Header.Set("X-Query-Snapshot", snapshot)
		}
	}, ModifyResponse: func(r *http.Response) error {
		if r.StatusCode >= 300 && r.StatusCode < 400 {
			return errors.New("upstream redirect rejected")
		}
		headers := make(http.Header)
		for _, key := range []string{"Content-Type", "X-Request-ID", "X-Query-Snapshot", "Retry-After"} {
			if value := r.Header.Get(key); value != "" {
				headers.Set(key, value)
			}
		}
		headers.Set("Cache-Control", "no-store")
		r.Header = headers
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		respond(w, 502, "unavailable", "Query service unavailable", nil)
	}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/console-api/query")
		allowed := r.Method == "GET" && (path == "/livez" || path == "/readyz")
		for _, op := range contract.Definitions() {
			allowed = allowed || r.Method == "POST" && path == op.Path
		}
		if !allowed || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		r = r.Clone(r.Context())
		r.URL.Path, r.URL.RawPath = path, ""
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		proxy.ServeHTTP(w, r)
	}), nil
}
