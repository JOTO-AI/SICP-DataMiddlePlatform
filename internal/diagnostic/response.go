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
	Unavailable string `json:"unavailable,omitempty"`
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

// Capture excludes business data and sensitive fields. Unfamiliar error
// envelopes use a bounded, recursively redacted fallback; HTML is plain text.
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
	if s == "" {
		r.Unavailable = "empty"
		return r
	}
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") || strings.Contains(r.ContentType, "json") {
		if text, ok := jsonErrorPreview(body, secrets); ok {
			s = text
		} else if !json.Valid(body) {
			s, r.Unavailable = "[JSON error response could not be captured completely]", "invalid_json"
		} else {
			s, r.Unavailable = "", "filtered"
		}
		if r.RequestID == "" {
			var object map[string]json.RawMessage
			if json.Unmarshal(body, &object) == nil {
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
	if r.Body == "" && r.Unavailable == "" {
		r.Unavailable = "filtered"
	}
	r.Truncated = r.Truncated || clipped
	return r
}

func (r Response) Valid() bool {
	return len(r.Unavailable) <= 64 && utf8.ValidString(r.Unavailable) && len(r.Body) <= PreviewBytes && len(r.RequestID) <= 128 && len(r.ContentType) <= 128 && utf8.ValidString(r.Body) && utf8.ValidString(r.RequestID) && utf8.ValidString(r.ContentType)
}

func (r Response) Sanitize(secrets ...string) Response {
	r.Body = Redact(r.Body, secrets...)
	r.RequestID = Redact(r.RequestID, secrets...)
	r.ContentType = Redact(r.ContentType, secrets...)
	r.Unavailable, _ = clip(Redact(r.Unavailable, secrets...), 64)
	var clipped bool
	r.Body, clipped = clip(r.Body, PreviewBytes)
	r.Truncated = r.Truncated || clipped
	r.RequestID, _ = clip(r.RequestID, 128)
	r.ContentType, _ = clip(r.ContentType, 128)
	return r
}

// Keys describe privacy boundaries, not a catalog of possible errors.
var privateJSONKey = regexp.MustCompile(`(?i)(token|secret|password|passwd|authorization|cookie|api.?key|app.?id|姓名|学号|手机号|身份证|^(data|payload|rows|records|list|stack|trace|sql|query|params|headers|request|xm|xh|yddh|dzxx|name|email|phone|mobile|student_number|staff_number)$)`)

func jsonErrorPreview(body []byte, secrets []string) (string, bool) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || !json.Valid(body) {
		return "", false
	}
	var scrub func(any, int) any
	scrub = func(v any, depth int) any {
		if depth > 8 {
			return "[nested content omitted]"
		}
		switch v := v.(type) {
		case map[string]any:
			out := map[string]any{}
			for key, item := range v {
				if privateJSONKey.MatchString(key) {
					continue
				}
				out[Redact(key, secrets...)] = scrub(item, depth+1)
			}
			return out
		case []any:
			out := make([]any, len(v))
			for i, item := range v {
				out[i] = scrub(item, depth+1)
			}
			return out
		case string:
			return Redact(v, secrets...)
		default:
			return v
		}
	}
	encoded, err := json.Marshal(scrub(value, 0))
	if err != nil || string(encoded) == "{}" || string(encoded) == "[]" || string(encoded) == "null" {
		return "", false
	}
	return string(encoded), true
}
