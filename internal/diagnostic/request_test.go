package diagnostic_test

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
)

func TestUnfamiliarErrorEnvelopeAndTransportAreSafe(t *testing.T) {
	response := diagnostic.Capture(http.Header{"Content-Type": {"application/json"}}, []byte(`{"code":"FUTURE_ERROR","problem":{"explanation":"new failure","credentials":{"password":"hidden"}},"data":{"XM":"Private Person"},"stack":"private trace"}`), false)
	if !strings.Contains(response.Body, "new failure") || strings.Contains(response.Body, "hidden") || strings.Contains(response.Body, "Private Person") || strings.Contains(response.Body, "private trace") {
		t.Fatal("unknown envelope lost or unsafe")
	}
	err := &url.Error{Op: "Get", URL: "https://private.invalid?secret=unknown-secret", Err: errors.New("TLS handshake failed https://private.invalid?secret=unknown-secret known-secret")}
	message, kind, _ := diagnostic.ErrorText(err, "known-secret")
	if !strings.Contains(message, "TLS handshake failed") || kind == "" || strings.Contains(message, "secret") || strings.Contains(message, "https://") {
		t.Fatal("unsafe URL error")
	}
}
