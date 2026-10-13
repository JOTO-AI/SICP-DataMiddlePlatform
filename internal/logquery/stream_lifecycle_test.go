package logquery

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type observingStreamWriter struct {
	streamWriter
	after func(string)
}

func (w *observingStreamWriter) Write(p []byte) (int, error) {
	n, err := w.streamWriter.Write(p)
	if err == nil && w.after != nil {
		w.after(string(p))
	}
	return n, err
}

func TestT30_04HeartbeatDoesNotAdvanceResumePosition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _, signer, now := readFixture(t)
		start := time.Now()
		svc, _ := NewService(readerFunc(func(context.Context, Source, ReadRequest) (Batch, error) {
			return Batch{Snapshot: Snapshot{"epoch-1", 8, 0}}, nil
		}), signer, s)
		svc.Now = func() time.Time { return now.Add(time.Since(start)) }
		server := NewStreamServer(svc)
		f := Filter{Mode: LiveMode, From: now.Add(-time.Hour), PageSize: 50}
		token, _ := signer.Mint(Platform, "epoch-1", Stream, f, "actor", 8, 8, 0, now)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		req := httptest.NewRequest("GET", "/stream?cursor="+token, nil).WithContext(ctx)
		req.SetPathValue("source", "platform")
		req.Header.Set("X-SICP-Actor", "actor")
		w := &observingStreamWriter{}
		w.after = func(p string) {
			if strings.Contains(p, ": heartbeat") {
				cancel()
			}
		}
		server.ServeHTTP(w, req)
		if !strings.Contains(w.body.String(), ": heartbeat\n\n") || time.Since(start) != 15*time.Second {
			t.Fatal("idle heartbeat missing")
		}
		for _, line := range strings.Split(w.body.String(), "\n") {
			if strings.HasPrefix(line, "id: ") && line != "id: "+token {
				t.Fatal("heartbeat or empty scan changed position")
			}
		}
		if len(server.connections) != 0 || len(svc.reads) != 0 {
			t.Fatal("cancelled idle stream leaked budget")
		}
	})
}

func TestT30_12EstablishedFailureNeverAdvancesControlFrame(t *testing.T) {
	for _, mode := range []string{"dependency", "retention", "epoch", "signature"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, rec, signer, now := readFixture(t)
				calls := 0
				expired := false
				svc, _ := NewService(readerFunc(func(_ context.Context, _ Source, req ReadRequest) (Batch, error) {
					calls++
					if calls == 1 {
						return Batch{Snapshot: Snapshot{"epoch-1", 9, 0}, Records: []Record{rec}}, nil
					}
					if mode == "dependency" {
						return Batch{}, ErrUnavailable
					}
					if mode == "signature" {
						return Batch{Snapshot: Snapshot{"epoch-1", 9, 0}}, nil
					}
					epoch, floor := "epoch-1", int64(10)
					if mode == "epoch" {
						epoch, floor = "recreated", 0
					}
					return Batch{}, req.Cursor.ValidateContinuation(req.Filter, epoch, floor)
				}), signer, s)
				svc.Now = func() time.Time {
					if expired {
						return now.Add(25 * time.Hour)
					}
					return now
				}
				f := Filter{Mode: LiveMode, From: now.Add(-time.Hour), PageSize: 50}
				token, _ := signer.Mint(Platform, "epoch-1", Stream, f, "actor", 8, 8, 0, now)
				req := httptest.NewRequest("GET", "/stream?cursor="+token, nil)
				req.SetPathValue("source", "platform")
				req.Header.Set("X-SICP-Actor", "actor")
				w := &observingStreamWriter{}
				w.after = func(p string) {
					if mode == "signature" && strings.Contains(p, "event: checkpoint") {
						expired = true
					}
				}
				server := NewStreamServer(svc)
				server.ServeHTTP(w, req)
				frames := strings.Split(strings.TrimSpace(w.body.String()), "\n\n")
				last := frames[len(frames)-1]
				if !strings.HasPrefix(last, "event: control\n") || strings.Contains(last, "id:") {
					t.Fatal("control advanced resume position", last)
				}
				want := "cursor_expired"
				if mode == "dependency" {
					want = "unavailable"
				}
				if !strings.Contains(last, want) {
					t.Fatal("wrong failure", last)
				}
				if len(server.connections) != 0 || len(svc.reads) != 0 {
					t.Fatal("failure leaked budget")
				}
			})
		})
	}
}

func TestT30_12ConnectionLifetimeRequestsResumption(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _, signer, now := readFixture(t)
		svc, _ := NewService(readerFunc(func(context.Context, Source, ReadRequest) (Batch, error) {
			return Batch{Snapshot: Snapshot{"epoch-1", 0, 0}}, nil
		}), signer, s)
		svc.Now = func() time.Time { return now }
		f := Filter{Mode: LiveMode, From: now.Add(-time.Hour), PageSize: 50}
		token, _ := signer.Mint(Platform, "epoch-1", Stream, f, "actor", 0, 0, 0, now)
		req := httptest.NewRequest("GET", "/stream?cursor="+token, nil)
		req.SetPathValue("source", "platform")
		req.Header.Set("X-SICP-Actor", "actor")
		w := &streamWriter{}
		start := time.Now()
		NewStreamServer(svc).ServeHTTP(w, req)
		if time.Since(start) > 5*time.Minute || !strings.Contains(w.body.String(), `"code":"connection_expired"`) {
			t.Fatal("connection lifetime unbounded")
		}
		if !strings.Contains(w.body.String(), `"rebuild_required":false`) {
			t.Fatal("connection lifetime invalidated history")
		}
	})
}
