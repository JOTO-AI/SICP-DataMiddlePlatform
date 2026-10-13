package adapters

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/notification"
)

var ErrMail = errors.New("mail submission failed or outcome unknown")

type Mail struct {
	dynamic                                 bool
	address, mode, user, password, from, to string
	timeout                                 time.Duration
}

func NewMail(c config.Runtime) (*Mail, error) {
	if !c.MailEnabled() {
		return nil, ErrMail
	}
	if e := c.Validate(); e != nil {
		return nil, ErrMail
	}
	return &Mail{address: c.MailAddress, mode: c.MailTLS, user: c.MailUser, password: c.MailPassword, from: c.MailFrom, to: c.MailTo, timeout: c.DependencyTimeout}, nil
}
func (m *Mail) SavedRecipients() *Mail { copy := *m; copy.dynamic = true; return &copy }
func beforeData(err error) (notification.Receipt, error) {
	var smtpError *textproto.Error
	if errors.As(err, &smtpError) && smtpError.Code >= 500 {
		return notification.Receipt{Rejected: true}, ErrMail
	}
	return notification.Receipt{Temporary: true}, ErrMail
}
func (*Mail) String() string              { return "[mail adapter redacted]" }
func (m *Mail) GoString() string          { return m.String() }
func (*Mail) Close(context.Context) error { return nil }
func (m *Mail) Send(ctx context.Context, v notification.Mail) (notification.Receipt, error) {
	address, e := mail.ParseAddress(v.To)
	if e != nil || address.Address != v.To || len(v.To) > 254 || strings.ContainsAny(v.To, "\r\n") || v.From != m.from || !m.dynamic && v.To != m.to || v.ID == "" || len(v.ID) > 128 || len(v.Subject) > 256 || len(v.Body) > 64<<10 || strings.ContainsAny(v.ID+v.Subject, "\r\n") {
		return notification.Receipt{}, ErrMail
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	d := net.Dialer{Timeout: m.timeout}
	conn, e := d.DialContext(ctx, "tcp", m.address)
	if e != nil {
		return beforeData(e)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if conn.SetDeadline(deadline) != nil {
		return beforeData(ErrMail)
	}
	host, _, _ := net.SplitHostPort(m.address)
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	wire := conn
	if m.mode == "tls" {
		secured := tls.Client(conn, tlsConfig)
		if e = secured.HandshakeContext(ctx); e != nil {
			return beforeData(e)
		}
		wire = secured
	}
	c, e := smtp.NewClient(wire, host)
	if e != nil {
		return beforeData(e)
	}
	defer c.Close()
	if m.mode == "starttls" {
		if e = c.StartTLS(tlsConfig); e != nil {
			return beforeData(e)
		}
	}
	if m.user != "" {
		if e = c.Auth(smtp.PlainAuth("", m.user, m.password, host)); e != nil {
			return beforeData(e)
		}
	}
	if e = c.Mail(v.From); e != nil {
		return beforeData(e)
	}
	if e = c.Rcpt(v.To); e != nil {
		return beforeData(e)
	}
	w, e := c.Data()
	if e != nil {
		return beforeData(e)
	}
	_, e = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMessage-ID: <%s@sicp.local>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", v.From, v.To, v.Subject, v.ID, v.Body)
	if e != nil {
		return notification.Receipt{}, ErrMail
	}
	if e = w.Close(); e != nil {
		var reply *textproto.Error
		if errors.As(e, &reply) {
			return beforeData(e)
		}
		return notification.Receipt{}, ErrMail
	}
	// The DATA acknowledgement proves acceptance, not delivery to a mailbox.
	return notification.Receipt{Accepted: true}, nil
}
