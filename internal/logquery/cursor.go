package logquery

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"
)

type Direction string

const (
	History Direction = "history"
	Stream  Direction = "stream"
)

// Sequence values are encoded as decimal strings for JavaScript precision.
type Cursor struct {
	Version         int       `json:"v"`
	Source          Source    `json:"source"`
	Epoch           string    `json:"epoch"`
	Direction       Direction `json:"direction"`
	Filter          Filter    `json:"filter"`
	Scope           string    `json:"scope"`
	HighWater       int64     `json:"high_water,string"`
	Position        int64     `json:"position,string"`
	RetainedThrough int64     `json:"retained_through,string"`
	IssuedAt        time.Time `json:"issued_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// Keys are immutable copies. Replace the signer atomically when rotating keys;
// retain old keys until their cursors expire, unless explicitly revoking them.
type Signer struct {
	active string
	keys   map[string][32]byte
}

func (Signer) String() string     { return "[log cursor signer redacted]" }
func (s Signer) GoString() string { return s.String() }

func NewSigner(key []byte) (*Signer, error) {
	return NewKeyring("default", map[string][]byte{"default": key})
}

func NewKeyring(active string, keys map[string][]byte) (*Signer, error) {
	if active == "" || len(keys) == 0 || len(keys) > 8 {
		return nil, ErrCursor
	}
	s := &Signer{active: active, keys: make(map[string][32]byte, len(keys))}
	for id, key := range keys {
		if id == "" || len(id) > 32 || !validIdentity(id) || len(key) != 32 || bytes.Equal(key, make([]byte, 32)) {
			return nil, ErrCursor
		}
		var copied [32]byte
		copy(copied[:], key)
		s.keys[id] = copied
	}
	if _, ok := s.keys[active]; !ok {
		return nil, ErrCursor
	}
	return s, nil
}

func scopeID(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(sum[:])
}

func (c Cursor) valid() bool {
	if c.Version != 1 || !c.Source.Valid() || c.Epoch == "" || !validIdentity(c.Epoch) || c.Filter.Validate() != nil || len(c.Scope) != 64 || c.HighWater < 0 || c.Position < 0 || c.RetainedThrough < 0 || c.RetainedThrough > c.HighWater || c.Position < c.RetainedThrough || !validTime(c.IssuedAt) || !validTime(c.ExpiresAt) || !c.ExpiresAt.After(c.IssuedAt) || c.ExpiresAt.Sub(c.IssuedAt) > MaxCursorAge {
		return false
	}
	if c.Filter.Mode == LiveMode && (c.Filter.From.After(c.IssuedAt) || c.IssuedAt.Sub(c.Filter.From) > MaxRange) {
		return false
	}
	if _, err := hex.DecodeString(c.Scope); err != nil {
		return false
	}
	if c.Direction == History {
		return c.Position <= c.HighWater
	}
	return c.Direction == Stream && c.Filter.Mode == LiveMode && c.Position >= c.HighWater
}

// Mint creates a new bounded snapshot. Continuation uses Encode with the
// original expiry; paging must never indefinitely extend a snapshot lease.
func (s *Signer) Mint(source Source, epoch string, direction Direction, filter Filter, scope string, highWater, position, retainedThrough int64, now time.Time) (string, error) {
	if scope == "" || len(scope) > 256 {
		return "", ErrCursor
	}
	now = now.UTC().Truncate(time.Microsecond)
	c := Cursor{Version: 1, Source: source, Epoch: epoch, Direction: direction, Filter: canonical(filter), Scope: scopeID(scope), HighWater: highWater, Position: position, RetainedThrough: retainedThrough, IssuedAt: now, ExpiresAt: now.Add(MaxCursorAge)}
	return s.Encode(c)
}

func (s *Signer) Encode(c Cursor) (string, error) {
	if s == nil || !c.valid() {
		return "", ErrCursor
	}
	key, ok := s.keys[s.active]
	if !ok {
		return "", ErrCursor
	}
	c.Filter = canonical(c.Filter)
	data, err := json.Marshal(c)
	if err != nil {
		return "", ErrCursor
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	unsigned := s.active + "." + payload
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte("sicp-log-cursor-v1." + unsigned))
	token := unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > MaxCursorBytes {
		return "", ErrCursor
	}
	return token, nil
}

// Decode authenticates before interpreting the payload. Authorization is still
// required on every HTTP request; a cursor is never a credential.
func (s *Signer) Decode(token string, source Source, direction Direction, scope string, now time.Time) (Cursor, error) {
	if s == nil || !source.Valid() || scope == "" || len(scope) > 256 || len(token) > MaxCursorBytes {
		return Cursor{}, ErrCursor
	}
	id, rest, ok := strings.Cut(token, ".")
	if !ok {
		return Cursor{}, ErrCursor
	}
	key, ok := s.keys[id]
	if !ok {
		return Cursor{}, ErrCursor
	}
	payload, signature, ok := strings.Cut(rest, ".")
	if !ok {
		return Cursor{}, ErrCursor
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(signature)
	if err != nil || base64.RawURLEncoding.EncodeToString(sig) != signature {
		return Cursor{}, ErrCursor
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte("sicp-log-cursor-v1." + id + "." + payload))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return Cursor{}, ErrCursor
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(payload)
	if err != nil || base64.RawURLEncoding.EncodeToString(data) != payload {
		return Cursor{}, ErrCursor
	}
	var c Cursor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || decoder.Decode(new(any)) != io.EOF || !c.valid() || c.Source != source || c.Direction != direction {
		return Cursor{}, ErrCursor
	}
	if c.Scope != scopeID(scope) {
		return Cursor{}, ErrForbidden
	}
	if !now.Before(c.ExpiresAt) {
		return Cursor{}, ErrExpired
	}
	if now.Before(c.IssuedAt) {
		return Cursor{}, ErrCursor
	}
	c.Filter = canonical(c.Filter)
	return c, nil
}

// ValidateContinuation prevents changing filters or paging size mid-snapshot.
// retainedThrough is the last sequence permanently removed by retention.
func (c Cursor) ValidateContinuation(filter Filter, epoch string, retainedThrough int64) error {
	if epoch == "" || !validIdentity(epoch) {
		return ErrCursor
	}
	if !c.valid() || filter.Validate() != nil || canonical(filter) != canonical(c.Filter) || retainedThrough < 0 {
		return ErrCursor
	}
	if epoch != c.Epoch {
		return ErrExpired
	}
	if retainedThrough < c.RetainedThrough {
		return ErrCursor
	}
	if c.Direction == History && retainedThrough > c.RetainedThrough || c.Direction == Stream && c.Position < retainedThrough {
		return ErrExpired
	}
	return nil
}

// StreamStart preserves the first history snapshot boundary and lease even
// when invoked on a later historical page. Historical position is not a live
// subscription offset. The caller still checks current epoch/floor separately.
func (c Cursor) StreamStart() (Cursor, error) {
	if !c.valid() || c.Direction != History || c.Filter.Mode != LiveMode {
		return Cursor{}, ErrCursor
	}
	c.Direction = Stream
	c.Position = c.HighWater
	return c, nil
}
