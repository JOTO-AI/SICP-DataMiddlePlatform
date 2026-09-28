package assembly

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT15_12AssemblyUsesExplicitSourceAndMailTargets(t *testing.T) {
	c, _ := fixture(t)
	var calls atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("access_token") != c.SourceToken || r.URL.Path != "/apizt/openapi/invoke/sjzc/release/dwd_xqjbsjzlb" {
			http.Error(w, "invalid mock request", 400)
			return
		}
		_, _ = io.WriteString(w, `{"code":200,"message":"ok","data":{"total":0,"list":[]}}`)
	}))
	defer source.Close()
	c.SourceURL = source.URL
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	c.MailAddress = listener.Addr().String()
	mailDone := make(chan string, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			mailDone <- "accept failed"
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)
		_, _ = io.WriteString(conn, "220 local mock SMTP\r\n")
		var transcript strings.Builder
		for {
			line, e := reader.ReadString('\n')
			if e != nil {
				mailDone <- transcript.String()
				return
			}
			transcript.WriteString(line)
			switch {
			case strings.HasPrefix(line, "EHLO "):
				_, _ = io.WriteString(conn, "250 local-mock\r\n")
			case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
				_, _ = io.WriteString(conn, "250 accepted\r\n")
			case line == "DATA\r\n":
				_, _ = io.WriteString(conn, "354 send data\r\n")
				for {
					part, e := reader.ReadString('\n')
					if e != nil {
						mailDone <- "data incomplete"
						return
					}
					if part == ".\r\n" {
						break
					}
					transcript.WriteString(part)
				}
				_, _ = io.WriteString(conn, "250 queued\r\n")
			default:
				_, _ = io.WriteString(conn, "500 unexpected mock command\r\n")
			}
		}
	}()
	var trace []string
	r, _, e := Open(t.Context(), c, trackedFactories(&trace, ""))
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = r.Close(ctx)
	}()
	p, e := r.Source.Fetch(t.Context(), ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: ingest.Full, Page: 1, PageSize: 2})
	if e != nil || p.Total != 0 || calls.Load() != 1 {
		t.Fatal(p, e, calls.Load())
	}
	receipt, e := r.Mail.Send(t.Context(), notification.Mail{ID: "synthetic-mail-id", From: c.MailFrom, To: c.MailTo, Subject: "Synthetic schema failure", Body: `{"failure":"synthetic"}`})
	if e != nil || !receipt.Accepted || receipt.Rejected || receipt.DeliveredAt != nil {
		t.Fatal(receipt, e)
	}
	select {
	case body := <-mailDone:
		for _, v := range []string{"MAIL FROM:<worker@example.invalid>", "RCPT TO:<operator@example.invalid>", "Message-ID: <synthetic-mail-id@sicp.local>", `{"failure":"synthetic"}`} {
			if !strings.Contains(body, v) {
				t.Fatal("mock did not receive expected mail", v)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mock mail did not finish")
	}
}

func TestT15_12RedirectDoesNotSendSourceTokenToOtherTarget(t *testing.T) {
	c, _ := fixture(t)
	var unexpected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { unexpected.Add(1) }))
	defer other.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer source.Close()
	c.SourceURL = source.URL
	s, e := RealFactories().Source(t.Context(), c)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(context.Background())
	if _, e = s.Fetch(t.Context(), ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: ingest.Full, Page: 1, PageSize: 2}); e == nil || unexpected.Load() != 0 {
		t.Fatal(e, unexpected.Load())
	}
}

func TestAssemblyObtainsSourceTokenFromApplicationCredentials(t *testing.T) {
	v := testkit.RuntimeValues("/data")
	v["SOURCE_TOKEN"] = ""
	v["SOURCE_APP_ID"], v["SOURCE_APP_SECRET"] = "synthetic-app", "synthetic-app-secret"
	v["ENVIRONMENT"], v["MAIL_TLS"] = "production", "starttls"
	c, err := config.LoadRuntime(config.Worker, func(k string) (string, bool) { value, ok := v[k]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	var auth, pages atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apizt/opentoken/oauth/token" {
			auth.Add(1)
			if r.URL.Query().Get("appId") != c.SourceAppID || r.URL.Query().Get("secret") != c.SourceAppSecret {
				t.Error("application credentials not wired")
			}
			io.WriteString(w, `{"code":200,"data":{"access_token":"issued-token","expires_in":3600}}`)
			return
		}
		pages.Add(1)
		if r.URL.Query().Get("access_token") != "issued-token" {
			t.Error("issued token not wired")
		}
		io.WriteString(w, `{"code":200,"message":"ok","data":{"total":0,"list":[]}}`)
	}))
	defer source.Close()
	c.SourceURL = source.URL
	s, err := RealFactories().Source(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	for page := 1; page <= 2; page++ {
		if _, err := s.Fetch(t.Context(), ingest.PageRequest{Table: "dwd_xqjbsjzlb", Mode: ingest.Full, Page: page, PageSize: 2}); err != nil {
			t.Fatal(err)
		}
	}
	if auth.Load() != 1 || pages.Load() != 2 {
		t.Fatal("unexpected authentication/page calls")
	}
}
func TestT15_12MailCancellationAndDestinationOverrideCannotSucceed(t *testing.T) {
	c, _ := fixture(t)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	c.MailAddress = l.Addr().String()
	m, e := RealFactories().Mail(t.Context(), c)
	if e != nil {
		t.Fatal(e)
	}
	message := notification.Mail{ID: "synthetic", From: c.MailFrom, To: "other@example.invalid", Subject: "mock", Body: "mock"}
	if out, e := m.Send(t.Context(), message); e == nil || out.Accepted {
		t.Fatal("destination override accepted")
	}
	accepted := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, e := l.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithCancel(t.Context())
	message.To = c.MailTo
	result := make(chan error, 1)
	go func() {
		out, e := m.Send(ctx, message)
		if out.Accepted || out.DeliveredAt != nil {
			result <- fmt.Errorf("canceled mail claimed success")
			return
		}
		result <- e
	}()
	select {
	case <-accepted:
		cancel()
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("mock not connected")
	}
	select {
	case e := <-result:
		if e == nil {
			t.Fatal("cancellation ignored")
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP cancellation unbounded")
	}
	<-done
}
