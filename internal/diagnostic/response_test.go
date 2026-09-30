package diagnostic_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
)

func TestJSONErrorRetainsExplanationWithoutCredentialsOrBusinessRows(t *testing.T) {
	body := []byte(`{"code":500,"message":"database connection pool exhausted; access_token=runtime-token; user@example.com; 13812345678","data":{"XM":"Private Person","XH":"student-private"},"stack":"private stack","request_id":"trace-example"}`)
	r := diagnostic.Capture(http.Header{"Content-Type": {"application/json"}}, body, false, "runtime-token")
	raw, _ := json.Marshal(r)
	for _, secret := range []string{"runtime-token", "user@example.com", "13812345678", "Private Person", "student-private", "private stack"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private field escaped", secret)
		}
	}
	if !strings.Contains(r.Body, "connection pool exhausted") || r.RequestID != "trace-example" || !r.Valid() {
		t.Fatal("useful error detail lost")
	}
}

func TestBoundedHTMLPartialAndMalformedResponses(t *testing.T) {
	r := diagnostic.Capture(http.Header{"Content-Type": {"text/html"}, "X-Request-Id": {"trace-html"}}, []byte("<h1>Gateway timeout</h1>"+strings.Repeat("超时 ", 5000)), true)
	if !r.Truncated || !r.ReadError || len(r.Body) > diagnostic.PreviewBytes || !utf8.ValidString(r.Body) || strings.Contains(r.Body, "<h1>") || !strings.Contains(r.Body, "Gateway timeout") {
		t.Fatal("unbounded or missing partial diagnostic")
	}
	r = diagnostic.Capture(http.Header{"Content-Type": {"application/json"}}, []byte(`{"data":{"XM":"Private Name"`), true)
	if strings.Contains(r.Body, "Private Name") {
		t.Fatal("malformed JSON leaked source fields")
	}
}

func TestSecretRedactionPrecedesTruncation(t *testing.T) {
	secret := "synthetic token+secret"
	r := diagnostic.Capture(http.Header{}, []byte(strings.Repeat("x", 2040)+secret), false, secret)
	if strings.Contains(r.Body, "synthetic") || !r.Truncated {
		t.Fatal("clipping exposed a credential prefix")
	}
}

func TestEmbeddedCredentialAssignmentsAndEncodedHTMLAreRedacted(t *testing.T) {
	r := diagnostic.Capture(http.Header{"Content-Type": {"text/html"}}, []byte(`<p>upstream unavailable password=&quot;private-password&quot; access_token=dynamic%2Btoken</p>`), false, "dynamic+token")
	if strings.Contains(r.Body, "private-password") || strings.Contains(r.Body, "dynamic") || !strings.Contains(r.Body, "upstream unavailable") {
		t.Fatal("embedded credentials leaked")
	}
}
