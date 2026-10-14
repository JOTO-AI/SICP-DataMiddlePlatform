// Package operations implements bounded management views and durable jobs.
package operations

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrArgument = errors.New("invalid management argument")
var ErrConflict = errors.New("management revision conflict")
var ErrUnavailable = errors.New("management dependency unavailable")
var ErrMissing = errors.New("management record not found")
var ErrLimit = errors.New("management budget exceeded")
var ErrForbidden = errors.New("management scope forbidden")

type Token struct {
	Events                   *EventFilter `json:"events,omitempty"`
	Scope, Subject, Position string
	Size                     int
	At, Expires              time.Time
}
type Tokens struct{ key [32]byte }

func NewTokens(secret string) *Tokens {
	if len(secret) < 32 {
		return nil
	}
	return &Tokens{key: sha256.Sum256([]byte("management-page-v1\x00" + secret))}
}
func (t *Tokens) Encode(v Token) (string, error) {
	if t == nil || v.Scope == "" || v.Subject == "" || v.Size < 1 || v.Size > 200 || !v.Expires.After(v.At) || v.Expires.Sub(v.At) > 24*time.Hour {
		return "", ErrArgument
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return "", ErrArgument
	}
	b := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, t.key[:])
	_, _ = mac.Write([]byte(b))
	return b + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (t *Tokens) Decode(raw, scope, subject string, now time.Time) (Token, error) {
	var v Token
	if t == nil || len(raw) > 4096 {
		return v, ErrArgument
	}
	p, s, ok := strings.Cut(raw, ".")
	if !ok {
		return v, ErrArgument
	}
	sig, e := base64.RawURLEncoding.Strict().DecodeString(s)
	if e != nil {
		return v, ErrArgument
	}
	mac := hmac.New(sha256.New, t.key[:])
	_, _ = mac.Write([]byte(p))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return v, ErrArgument
	}
	data, e := base64.RawURLEncoding.Strict().DecodeString(p)
	if e != nil || json.Unmarshal(data, &v) != nil || v.Scope != scope || v.Size < 1 || v.Size > 200 {
		return Token{}, ErrArgument
	}
	if v.Subject != subject {
		return Token{}, ErrForbidden
	}
	if !now.Before(v.Expires) || now.Before(v.At) {
		return Token{}, ErrConflict
	}
	return v, nil
}
func ValidID(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == ':') {
			return false
		}
	}
	return true
}
