// Package value binds source tokens without floating point conversion.
package value

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Kind string

const (
	Text      Kind = "TEXT"
	JSON      Kind = "JSON"
	Timestamp Kind = "TIMESTAMP WITHOUT TIME ZONE"
)

// Field retains source key presence. SQL TEXT/timestamp alone cannot preserve
// the distinction between a missing key and an explicit null.
type Field struct {
	Present bool
	Raw     json.RawMessage
}

var ErrInvalid = errors.New("invalid source value for storage type")
var localTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d{1,6})?$`)

// Bind returns nil for SQL NULL, a string for TEXT/JSON, or a time.Time carrying
// naive calendar fields for a PostgreSQL timestamp (never timestamptz) parameter.
// Callers bind JSON as JSON, not JSONB, and must not marshal the returned string.
func Bind(kind Kind, field Field) (any, error) {
	if kind != Text && kind != JSON && kind != Timestamp {
		return nil, ErrInvalid
	}
	if !field.Present {
		if len(field.Raw) != 0 {
			return nil, ErrInvalid
		}
		return nil, nil
	}
	raw := bytes.TrimSpace(field.Raw)
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, ErrInvalid
	}
	if kind == JSON {
		return string(raw), nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	text, err := decodeString(raw)
	if err != nil {
		return nil, err
	}
	if kind == Text {
		return text, nil
	}
	if !localTime.MatchString(text) {
		return nil, ErrInvalid
	}
	parsed, err := time.Parse("2006-01-02T15:04:05.999999", strings.Replace(text, " ", "T", 1))
	if err != nil || parsed.Year() < 1 {
		return nil, ErrInvalid
	}
	return parsed, nil
}

// Restore returns nil for SQL NULL and the original JSON token for JSON null.
// JSON columns must be selected as ::text and scanned into a nullable string.
// Timestamp representation is canonicalized; the calendar value is preserved.
func Restore(kind Kind, stored any) (json.RawMessage, error) {
	if kind != Text && kind != JSON && kind != Timestamp {
		return nil, ErrInvalid
	}
	if stored == nil {
		return nil, nil
	}
	if kind == Timestamp {
		v, ok := stored.(time.Time)
		if !ok || v.Year() < 1 || v.Year() > 9999 || v.Nanosecond()%1000 != 0 {
			return nil, ErrInvalid
		}
		raw, _ := json.Marshal(v.Format("2006-01-02T15:04:05.999999"))
		return raw, nil
	}
	v, ok := stored.(string)
	if !ok || !utf8.ValidString(v) {
		return nil, ErrInvalid
	}
	if kind == JSON {
		if !json.Valid([]byte(v)) {
			return nil, ErrInvalid
		}
		return json.RawMessage(v), nil
	}
	if strings.ContainsRune(v, 0) {
		return nil, ErrInvalid
	}
	raw, _ := json.Marshal(v)
	return raw, nil
}

func decodeString(raw []byte) (string, error) {
	if len(raw) < 2 || raw[0] != '"' {
		return "", ErrInvalid
	}
	// encoding/json replaces isolated UTF-16 surrogates. Reject them before
	// unmarshalling so PostgreSQL TEXT never receives silently changed content.
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return "", ErrInvalid
		}
		i += 4
		if code >= 0xDC00 && code <= 0xDFFF {
			return "", ErrInvalid
		}
		if code >= 0xD800 && code <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return "", ErrInvalid
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xDC00 || low > 0xDFFF {
				return "", ErrInvalid
			}
			i += 6
		}
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || strings.ContainsRune(value, 0) {
		return "", ErrInvalid
	}
	return value, nil
}
