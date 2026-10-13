package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/streamio"
)

// LogRoutes is mounted only on query-service and protected with a credential
// independent of both business keys and browser sessions.
func LogRoutes(service *logquery.Service, stream http.Handler, queryKey, token string) (http.Handler, error) {
	if service == nil {
		return nil, errors.New("log service missing")
	}
	auth, err := NewAuth(queryKey, []Credential{{Operator: "log-proxy", Token: token, Actions: []Action{LogsRead}}})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/admin/v1/logs/{source}/events", func(w http.ResponseWriter, r *http.Request) {
		values, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			logFailure(w, logquery.ErrArgument)
			return
		}
		out, err := service.List(r.Context(), logquery.Source(r.PathValue("source")), r.Header.Get("X-SICP-Actor"), values)
		if err != nil {
			logFailure(w, err)
			return
		}
		respond(w, 200, "ok", "", out)
	})
	mux.HandleFunc("GET /internal/admin/v1/logs/{source}/events/{event_id}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			logFailure(w, logquery.ErrArgument)
			return
		}
		out, err := service.Detail(r.Context(), logquery.Source(r.PathValue("source")), r.PathValue("event_id"))
		if err != nil {
			logFailure(w, err)
			return
		}
		respond(w, 200, "ok", "", out)
	})
	if stream != nil {
		mux.Handle("GET /internal/admin/v1/logs/{source}/stream", stream)
	}
	return auth.Protect(LogsRead, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := r.Header.Values("X-SICP-Actor")
		if len(actor) != 1 || len(actor[0]) != 64 {
			logFailure(w, logquery.ErrForbidden)
			return
		}
		if _, e := hex.DecodeString(actor[0]); e != nil {
			logFailure(w, logquery.ErrForbidden)
			return
		}
		if !logquery.Source(strings.Split(strings.TrimPrefix(r.URL.Path, "/internal/admin/v1/logs/"), "/")[0]).Valid() {
			logFailure(w, logquery.ErrArgument)
			return
		}
		if len(r.URL.RawQuery) > 16<<10 {
			logFailure(w, logquery.ErrTooLarge)
			return
		}
		mux.ServeHTTP(w, r)
	}))
}
func logFailure(w http.ResponseWriter, err error) {
	status, code := logquery.HTTPError(err)
	if status == 429 {
		w.Header().Set("Retry-After", "1")
	}
	respond(w, status, code, "Management log request failed", nil)
}

// LogProxy owns the destination, credentials, path shape and header allowlist.
// Each SSE write has a deadline; periodic session validation cancels the
// upstream request even when it is idle or the session was logged out.
func LogProxy(address, token string, sessions *Sessions) (http.Handler, error) {
	return logProxy(address, token, sessions, 15*time.Second)
}

func logProxy(address, token string, sessions *Sessions, recheck time.Duration) (http.Handler, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") || token == "" || sessions == nil || recheck <= 0 {
		return nil, errors.New("invalid log proxy configuration")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 5 * time.Second
	transport.MaxConnsPerHost = 24
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	streams, reads := make(chan struct{}, 16), make(chan struct{}, 8)
	return sessions.Protect(LogsRead, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/v1/logs/"), "/")
		if r.Method != "GET" || len(parts) < 2 || len(parts) > 3 || !logquery.Source(parts[0]).Valid() || (parts[1] != "events" && parts[1] != "stream") || (parts[1] == "stream" && len(parts) != 2) {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 3 {
			if len(parts[2]) != 64 {
				logFailure(w, logquery.ErrArgument)
				return
			}
			if _, err := hex.DecodeString(parts[2]); err != nil {
				logFailure(w, logquery.ErrArgument)
				return
			}
		}
		if len(r.URL.RawQuery) > 16<<10 || len(r.Header.Values("Last-Event-ID")) > 1 || len(r.Header.Get("Last-Event-ID")) > logquery.MaxCursorBytes {
			logFailure(w, logquery.ErrTooLarge)
			return
		}
		streaming := parts[1] == "stream"
		budget := reads
		if streaming {
			budget = streams
		}
		select {
		case budget <- struct{}{}:
			defer func() { <-budget }()
		default:
			logFailure(w, logquery.ErrLimit)
			return
		}
		timeout := 5 * time.Second
		if streaming {
			timeout = 5 * time.Minute
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		var output *streamio.Writer
		if streaming {
			output = streamio.New(ctx, w, 5*time.Second)
			defer output.Close()
			done := make(chan struct{})
			defer func() { cancel(); <-done }()
			go func() {
				defer close(done)
				ticker := time.NewTicker(recheck)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if _, ok := sessions.lookup(r); !ok {
							cancel()
							return
						}
					}
				}
			}()
		}
		target := *u
		target.Path = "/internal" + r.URL.Path
		target.RawPath = ""
		target.RawQuery = r.URL.RawQuery
		req, err := http.NewRequestWithContext(ctx, "GET", target.String(), nil)
		if err != nil {
			logFailure(w, logquery.ErrArgument)
			return
		}
		operator, _, _ := Identity(r.Context())
		actor := sha256.Sum256([]byte(operator))
		req.Header.Set("X-SICP-Actor", hex.EncodeToString(actor[:]))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Last-Event-ID", r.Header.Get("Last-Event-ID"))
		resp, err := client.Do(req)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				logFailure(w, logquery.ErrTimeout)
			} else {
				logFailure(w, logquery.ErrProxy)
			}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 && resp.StatusCode < 400 || resp.StatusCode < 200 || resp.StatusCode >= 600 {
			logFailure(w, logquery.ErrProxy)
			return
		}
		if !streaming || resp.StatusCode != 200 {
			data, e := io.ReadAll(io.LimitReader(resp.Body, logquery.MaxResponseBytes+1))
			if e != nil || len(data) > logquery.MaxResponseBytes || !json.Valid(data) || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
				logFailure(w, logquery.ErrProxy)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			if resp.StatusCode == 429 {
				w.Header().Set("Retry-After", "1")
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(data)
			return
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			logFailure(w, logquery.ErrProxy)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		buffer := make([]byte, 32<<10)
		for {
			n, e := resp.Body.Read(buffer)
			if ctx.Err() != nil {
				return
			}
			if n > 0 {
				if output.Prepare() != nil {
					return
				}
				if _, err = output.Write(buffer[:n]); err != nil {
					return
				}
				if output.Flush() != nil {
					return
				}
			}
			if e != nil {
				return
			}
		}
	}))
}
