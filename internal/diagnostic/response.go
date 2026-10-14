// Package diagnostic retains bounded, redacted upstream error evidence.
package diagnostic

import (
	"encoding/json"
	"html"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const CaptureBytes = 16 << 10
const PreviewBytes = 2048

type Response struct {
	Body        string `json:"body,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	ReadError   bool   `json:"read_error,omitempty"`
}

func (Response) String() string     { return "[upstream diagnostic redacted]" }
func (r Response) GoString() string { return r.String() }

var tags = regexp.MustCompile(`<[^>]*>`)
var htmlNoise = regexp.MustCompile(`(?is)<!--.*?-->|<script\b[^>]*>.*?</script\s*>|<style\b[^>]*>.*?</style\s*>`)
var credentials = regexp.MustCompile(`(?i)(?:bearer\s+|(?:access[_-]?token|refresh[_-]?token|authorization|password|passwd|secret|api[_-]?key|cookie|姓名|学号|手机号|身份证|\bXM|\bXH|\bYDDH|\bDZXX)["']?\s*[=:：]\s*)(?:"[^"]*"|'[^']*'|[^\s&,;<>]+)`)
var email = regexp.MustCompile(`[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}`)
var numbers = regexp.MustCompile(`\b[0-9]{7,}[0-9Xx]?\b`)
var quoted = regexp.MustCompile(`'[^']*'`)

// Redact removes configured credentials (including URL/JSON encodings), common
// credential assignments, personal identifiers and quoted SQL values. It is
// deliberately applied before truncation so clipping cannot expose a secret.
func Redact(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		encoded, _ := json.Marshal(secret)
		for _, value := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret), string(encoded[1 : len(encoded)-1])} {
			s = strings.ReplaceAll(s, value, "[redacted]")
		}
	}
	s = credentials.ReplaceAllString(s, "[redacted]")
	s = email.ReplaceAllString(s, "[redacted]")
	s = numbers.ReplaceAllString(s, "[redacted]")
	s = quoted.ReplaceAllString(s, "'[redacted]'")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
	return strings.TrimSpace(s)
}

func clip(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}

// Capture never preserves success data, arbitrary JSON fields or response
// headers. JSON errors retain only provider error fields; HTML is plain text.
func Capture(h http.Header, body []byte, readError bool, secrets ...string) *Response {
	r := &Response{ReadError: readError}
	if media, _, err := mime.ParseMediaType(h.Get("Content-Type")); err == nil {
		r.ContentType, _ = clip(Redact(media, secrets...), 128)
	}
	for _, name := range []string{"X-Request-ID", "X-Correlation-ID", "Traceparent"} {
		if value := h.Get(name); value != "" {
			r.RequestID, _ = clip(Redact(value, secrets...), 128)
			break
		}
	}
	r.Truncated = len(body) > CaptureBytes
	if r.Truncated {
		body = body[:CaptureBytes]
	}
	s := strings.TrimSpace(string(body))
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") || strings.Contains(r.ContentType, "json") {
		var object map[string]json.RawMessage
		if json.Unmarshal(body, &object) != nil {
			s = "[JSON error response could not be captured completely]"
		} else {
			parts := []string{}
			for _, name := range []string{"code", "message", "msg", "error", "detail", "error_description"} {
				var value string
				if json.Unmarshal(object[name], &value) == nil {
					parts = append(parts, name+": "+value)
					continue
				}
				if name == "code" {
					var code json.Number
					if json.Unmarshal(object[name], &code) == nil && code != "" {
						parts = append(parts, "code: "+string(code))
					}
				}
				if name == "error" {
					var nested struct {
						Message string `json:"message"`
					}
					if json.Unmarshal(object[name], &nested) == nil && nested.Message != "" {
						parts = append(parts, "error: "+nested.Message)
					}
				}
			}
			s = strings.Join(parts, "\n")
			if r.RequestID == "" {
				for _, name := range []string{"request_id", "requestId", "traceId"} {
					var value string
					if json.Unmarshal(object[name], &value) == nil && value != "" {
						r.RequestID, _ = clip(Redact(value, secrets...), 128)
						break
					}
				}
			}
		}
	} else {
		if r.Truncated {
			if at := strings.LastIndexAny(s, " \n\t"); at >= 0 {
				s = s[:at]
			} else {
				s = "[response exceeds capture limit]"
			}
		}
		s = html.UnescapeString(tags.ReplaceAllString(htmlNoise.ReplaceAllString(s, " "), " "))
	}
	var clipped bool
	r.Body, clipped = clip(Redact(s, secrets...), PreviewBytes)
	r.Truncated = r.Truncated || clipped
	return r
}

func (r Response) Valid() bool {
	return len(r.Body) <= PreviewBytes && len(r.RequestID) <= 128 && len(r.ContentType) <= 128 && utf8.ValidString(r.Body) && utf8.ValidString(r.RequestID) && utf8.ValidString(r.ContentType)
}

func (r Response) Sanitize(secrets ...string) Response {
	r.Body = Redact(r.Body, secrets...)
	r.RequestID = Redact(r.RequestID, secrets...)
	r.ContentType = Redact(r.ContentType, secrets...)
	var clipped bool
	r.Body, clipped = clip(r.Body, PreviewBytes)
	r.Truncated = r.Truncated || clipped
	r.RequestID, _ = clip(r.RequestID, 128)
	r.ContentType, _ = clip(r.ContentType, 128)
	return r
}
