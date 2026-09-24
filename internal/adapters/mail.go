package adapters

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
)

var ErrMail = errors.New("mail submission failed or outcome unknown")

type Mail struct {
	address, mode, user, password, from, to string
	timeout                                 time.Duration
}

func NewMail(c config.Runtime) (*Mail, error) {
	if e := c.Validate(); e != nil {
		return nil, ErrMail
	}
	return &Mail{c.MailAddress, c.MailTLS, c.MailUser, c.MailPassword, c.MailFrom, c.MailTo, c.DependencyTimeout}, nil
}
func (*Mail) String() string              { return "[mail adapter redacted]" }
func (m *Mail) GoString() string          { return m.String() }
func (*Mail) Close(context.Context) error { return nil }
func (m *Mail) Send(ctx context.Context, v notification.Mail) (notification.Receipt, error) {
	if v.From != m.from || v.To != m.to || v.ID == "" || len(v.ID) > 128 || len(v.Subject) > 256 || len(v.Body) > 64<<10 || strings.ContainsAny(v.ID+v.Subject, "\r\n") {
		return notification.Receipt{}, ErrMail
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	d := net.Dialer{Timeout: m.timeout}
	conn, e := d.DialContext(ctx, "tcp", m.address)
	if e != nil {
		return notification.Receipt{}, ErrMail
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if conn.SetDeadline(deadline) != nil {
		return notification.Receipt{}, ErrMail
	}
	host, _, _ := net.SplitHostPort(m.address)
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	wire := conn
	if m.mode == "tls" {
		secured := tls.Client(conn, tlsConfig)
		if secured.HandshakeContext(ctx) != nil {
			return notification.Receipt{}, ErrMail
		}
		wire = secured
	}
	c, e := smtp.NewClient(wire, host)
	if e != nil {
		return notification.Receipt{}, ErrMail
	}
	defer c.Close()
	if m.mode == "starttls" {
		if e = c.StartTLS(tlsConfig); e != nil {
			return notification.Receipt{}, ErrMail
		}
	}
	if m.user != "" {
		if e = c.Auth(smtp.PlainAuth("", m.user, m.password, host)); e != nil {
			return notification.Receipt{}, ErrMail
		}
	}
	if c.Mail(v.From) != nil || c.Rcpt(v.To) != nil {
		return notification.Receipt{}, ErrMail
	}
	w, e := c.Data()
	if e != nil {
		return notification.Receipt{}, ErrMail
	}
	_, e = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMessage-ID: <%s@sicp.local>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", v.From, v.To, v.Subject, v.ID, v.Body)
	if e != nil {
		return notification.Receipt{}, ErrMail
	}
	if w.Close() != nil || ctx.Err() != nil {
		return notification.Receipt{}, ErrMail
	}
	// The DATA acknowledgement proves acceptance, not delivery to a mailbox.
	return notification.Receipt{Accepted: true}, nil
}
