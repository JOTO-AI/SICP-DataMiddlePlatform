package logquery

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type streamWriter struct {
	header                             http.Header
	body                               bytes.Buffer
	status                             int
	writes, failAt, deadlines, flushes int
	cancel                             context.CancelFunc
}

func (w *streamWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *streamWriter) WriteHeader(status int) { w.status = status }
func (w *streamWriter) Write(b []byte) (int, error) {
	w.writes++
	if w.failAt > 0 && w.writes >= w.failAt {
		return 0, errors.New("slow client disconnected")
	}
	n, e := w.body.Write(b)
	if bytes.Contains(b, []byte("event: checkpoint")) && w.cancel != nil {
		w.cancel()
	}
	return n, e
}
func (w *streamWriter) SetWriteDeadline(time.Time) error { w.deadlines++; return nil }
func (w *streamWriter) FlushError() error                { w.flushes++; return nil }

func TestT30_04To06OrderedFramesWriteFailureAndConnectionRelease(t *testing.T) {
	for _, failAt := range []int{0, 2} {
		t.Run(string(rune('a'+failAt)), func(t *testing.T) {
			s, rec, signer, now := readFixture(t)
			svc, _ := NewService(readerFunc(func(context.Context, Source, ReadRequest) (Batch, error) {
				return Batch{Snapshot: Snapshot{"epoch-1", 9, 0}, Records: []Record{rec}}, nil
			}), signer, s)
			svc.Now = func() time.Time { return now }
			server := NewStreamServer(svc)
			f := Filter{Mode: LiveMode, From: now.Add(-time.Hour), PageSize: 50}
			token, _ := signer.Mint(Platform, "epoch-1", Stream, f, "actor", 8, 8, 0, now)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req := httptest.NewRequest("GET", "/stream?cursor="+token, nil).WithContext(ctx)
			req.SetPathValue("source", "platform")
			req.Header.Set("X-SICP-Actor", "actor")
			w := &streamWriter{failAt: failAt, cancel: cancel}
			server.ServeHTTP(w, req)
			if len(server.connections) != 0 || len(svc.reads) != 0 || len(server.sources[Platform]) != 0 {
				t.Fatal("stream leaked budget")
			}
			if w.deadlines == 0 {
				t.Fatal("no finite write deadline")
			}
			body := w.body.String()
			if failAt == 0 {
				if !strings.Contains(body, "event: event\n") || !strings.Contains(body, "event: checkpoint\n") || strings.Index(body, "event: event\n") > strings.Index(body, "event: checkpoint\n") {
					t.Fatal("checkpoint preceded matching event")
				}
				if w.flushes < 2 {
					t.Fatal("frames buffered")
				}
			} else if strings.Contains(body, "checkpoint") {
				t.Fatal("failed matching write advanced checkpoint")
			}
		})
	}
}
func TestT30_13ConnectionBudgetRejectsBeforeRead(t *testing.T) {
	s, _, signer, now := readFixture(t)
	svc, _ := NewService(readerFunc(func(context.Context, Source, ReadRequest) (Batch, error) {
		t.Fatal("over-limit subscription read database")
		return Batch{}, nil
	}), signer, s)
	svc.Now = func() time.Time { return now }
	server := NewStreamServer(svc)
	for range 16 {
		server.connections <- struct{}{}
	}
	f := Filter{Mode: LiveMode, From: now.Add(-time.Hour), PageSize: 50}
	token, _ := signer.Mint(Platform, "epoch-1", Stream, f, "actor", 0, 0, 0, now)
	req := httptest.NewRequest("GET", "/stream?cursor="+token, nil)
	req.SetPathValue("source", "platform")
	req.Header.Set("X-SICP-Actor", "actor")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("missing admission budget")
	}
}

func TestT30_01And02PersistentBoundaryAcrossServices(t *testing.T) {
	s, rec, signer, now := readFixture(t)
	f := Filter{Mode: LiveMode, From: now.Add(-24 * time.Hour), PageSize: 50}
	token, e := signer.Mint(Platform, "epoch-1", History, f, "actor", 8, 8, 0, now)
	if e != nil {
		t.Fatal(e)
	}
	c, e := signer.Decode(token, Platform, History, "actor", now)
	if e != nil {
		t.Fatal(e)
	}
	c, e = c.StreamStart()
	if e != nil {
		t.Fatal(e)
	}
	for range 2 {
		svc, _ := NewService(readerFunc(func(_ context.Context, _ Source, r ReadRequest) (Batch, error) {
			if r.Cursor.Position != 8 || !r.Stream {
				t.Fatal("did not resume from H")
			}
			return Batch{Snapshot: Snapshot{"epoch-1", 9, 0}, Records: []Record{rec}}, nil
		}), signer, s)
		server := NewStreamServer(svc)
		b, e := server.scan(t.Context(), c)
		if e != nil || len(b.Records) != 1 || !matches(f, rec) {
			t.Fatal(e)
		}
	}
	f.From = now
	if matches(f, rec) {
		t.Fatal("event before fixed from matched")
	}
}
func TestT30_03SharedScanAndIndependentFilters(t *testing.T) {
	s, rec, signer, now := readFixture(t)
	calls := 0
	svc, _ := NewService(readerFunc(func(context.Context, Source, ReadRequest) (Batch, error) {
		calls++
		return Batch{Snapshot: Snapshot{"epoch-1", 9, 0}, Records: []Record{rec}}, nil
	}), signer, s)
	f := Filter{Mode: LiveMode, From: now.Add(-time.Hour), PageSize: 50}
	token, _ := signer.Mint(Platform, "epoch-1", Stream, f, "actor", 8, 8, 0, now)
	c, _ := signer.Decode(token, Platform, Stream, "actor", now)
	server := NewStreamServer(svc)
	if _, e := server.scan(t.Context(), c); e != nil {
		t.Fatal(e)
	}
	c.Filter.RequestID = strings.Repeat("f", 64)
	b, e := server.scan(t.Context(), c)
	if e != nil || calls != 1 || matches(c.Filter, b.Records[0]) {
		t.Fatal("scan not shared or filter leaked", e)
	}
	c.RetainedThrough = 1
	if _, e := server.scan(t.Context(), c); e == nil {
		t.Fatal("invalid continuation")
	}
}
func TestT30_11InvalidLastEventIDNeverFallsBack(t *testing.T) {
	s, _, signer, now := readFixture(t)
	calls := 0
	svc, _ := NewService(readerFunc(func(context.Context, Source, ReadRequest) (Batch, error) { calls++; return Batch{}, nil }), signer, s)
	svc.Now = func() time.Time { return now }
	f := Filter{Mode: LiveMode, From: now.Add(-time.Hour), PageSize: 50}
	token, _ := signer.Mint(Platform, "epoch-1", Stream, f, "actor", 0, 0, 0, now)
	req := httptest.NewRequest("GET", "/internal/admin/v1/logs/platform/stream?cursor="+token, nil)
	req.SetPathValue("source", "platform")
	req.Header.Set("X-SICP-Actor", "actor")
	req.Header.Set("Last-Event-ID", "broken")
	w := httptest.NewRecorder()
	NewStreamServer(svc).ServeHTTP(w, req)
	if w.Code != 400 || calls != 0 {
		t.Fatal("fell back to URL cursor")
	}
}
func TestT30_12ExpiredAndHistoryCursorsRefuseStream(t *testing.T) {
	s, _, signer, now := readFixture(t)
	svc, _ := NewService(readerFunc(func(context.Context, Source, ReadRequest) (Batch, error) {
		t.Fatal("unexpected read")
		return Batch{}, nil
	}), signer, s)
	for _, direction := range []Direction{History, Stream} {
		f := Filter{Mode: LiveMode, From: now.Add(-time.Hour), PageSize: 50}
		token, _ := signer.Mint(Platform, "epoch-1", direction, f, "actor", 0, 0, 0, now)
		svc.Now = func() time.Time { return now.Add(25 * time.Hour) }
		req := httptest.NewRequest("GET", "/stream?cursor="+token, nil)
		req.SetPathValue("source", "platform")
		req.Header.Set("X-SICP-Actor", "actor")
		w := httptest.NewRecorder()
		NewStreamServer(svc).ServeHTTP(w, req)
		want := 409
		if direction == History {
			want = 400
		}
		if w.Code != want {
			t.Fatal(w.Code)
		}
	}
}
