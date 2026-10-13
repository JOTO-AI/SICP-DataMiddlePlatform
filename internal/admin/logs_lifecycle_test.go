package admin

import (
	"bufio"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestT30_07To09ProxyLogoutAndClientCancellationCloseUpstream(t *testing.T) {
	for _, mode := range []string{"logout", "client_cancel"} {
		t.Run(mode, func(t *testing.T) {
			ended := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(ended)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, ": connected\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer upstream.Close()
			cookie := strings.Repeat("c", 43)
			hash := sha256.Sum256([]byte(cookie))
			s := &Sessions{operator: "synthetic-admin", sessions: map[[32]byte]session{hash: {expires: time.Now().Add(time.Hour)}}, now: time.Now}
			h, e := logProxy(upstream.URL, strings.Repeat("x", 32), s, 20*time.Millisecond)
			if e != nil {
				t.Fatal(e)
			}
			proxy := httptest.NewServer(h)
			defer proxy.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", proxy.URL+"/admin/v1/logs/platform/stream?cursor=synthetic", nil)
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
			resp, e := http.DefaultClient.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			defer resp.Body.Close()
			if line, e := bufio.NewReader(resp.Body).ReadString('\n'); e != nil || line != ": connected\n" {
				t.Fatal("proxy did not flush", e)
			}
			if mode == "logout" {
				s.mu.Lock()
				delete(s.sessions, hash)
				s.mu.Unlock()
			} else {
				cancel()
			}
			select {
			case <-ended:
			case <-time.After(2 * time.Second):
				t.Fatal("upstream subscription leaked")
			}
		})
	}
}
