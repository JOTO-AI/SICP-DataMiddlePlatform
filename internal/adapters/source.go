package adapters

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

type Source struct {
	*ingest.Client
	transport *http.Transport
}

func OpenSource(origin, token string, timeout time.Duration) (*Source, error) {
	t := &http.Transport{DialContext: (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, IdleConnTimeout: time.Minute, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 4}
	c, e := ingest.NewClient(origin, token, t, 64<<20)
	if e != nil {
		t.CloseIdleConnections()
		return nil, e
	}
	return &Source{c, t}, nil
}
func (s *Source) Close(context.Context) error { s.transport.CloseIdleConnections(); return nil }
