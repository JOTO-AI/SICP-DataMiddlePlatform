package admin

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const SessionCookie = "sicp_session"
const CSRFHeader = "X-SICP-CSRF"
const sessionTTL = 8 * time.Hour

type session struct {
	expires time.Time
	csrf    string
}

// Sessions are process-local: worker restarts and credential changes revoke them.
// Only digests of passwords and opaque session identifiers are retained.
type Sessions struct {
	mu       sync.Mutex
	operator string
	username [32]byte
	password []byte
	salt     []byte
	secure   bool
	sessions map[[32]byte]session
	attempts int
	window   time.Time
	now      func() time.Time
}

func ValidAccount(username, password, queryKey string) bool {
	return username != "" && len(username) <= 128 && strings.TrimSpace(username) == username &&
		strings.IndexFunc(username, func(r rune) bool { return r < 32 || r == 127 }) < 0 &&
		len(password) >= 12 && len(password) <= 1024 && strings.TrimSpace(password) != "" && password != queryKey
}

func NewSessions(username, password, queryKey string, secure bool) (*Sessions, error) {
	if !ValidAccount(username, password, queryKey) {
		return nil, errors.New("invalid administrator account")
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	digest, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		return nil, err
	}
	return &Sessions{operator: username, username: sha256.Sum256([]byte(username)), password: digest, salt: salt, secure: secure, sessions: map[[32]byte]session{}, now: time.Now}, nil
}

// A non-simple header on all writes prevents cross-origin form submissions,
// including login CSRF. No CORS access is granted by this service.
func consoleWrite(r *http.Request) bool {
	return r.Header.Get("X-SICP-Request") == "console" && r.Header.Get("Sec-Fetch-Site") != "cross-site"
}

func (s *Sessions) lookup(r *http.Request) (session, bool) {
	cookies := r.CookiesNamed(SessionCookie)
	if len(cookies) != 1 || len(cookies[0].Value) != 43 {
		return session{}, false
	}
	key := sha256.Sum256([]byte(cookies[0].Value))
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[key]
	if !ok || !s.now().Before(v.expires) {
		delete(s.sessions, key)
		return session{}, false
	}
	return v, true
}

func (s *Sessions) authorize(w http.ResponseWriter, r *http.Request) bool {
	v, ok := s.lookup(r)
	if !ok {
		respond(w, 401, "unauthenticated", "Login required", nil)
		return false
	}
	if r.Method != "GET" && r.Method != "HEAD" && (!consoleWrite(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get(CSRFHeader)), []byte(v.csrf)) != 1) {
		respond(w, 403, "forbidden", "Invalid session request", nil)
		return false
	}
	return true
}

func (s *Sessions) Protect(action Action, next http.Handler) (http.Handler, error) {
	if !validAction(action) || next == nil {
		return nil, errors.New("invalid management action")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(w, r) {
			return
		}
		ctx := context.WithValue(r.Context(), identityKey{}, identity{operator: s.operator, action: action})
		next.ServeHTTP(w, r.WithContext(ctx))
	}), nil
}

func (s *Sessions) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorize(w, r) {
			next.ServeHTTP(w, r)
		}
	})
}

func (s *Sessions) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

func (s *Sessions) Login(w http.ResponseWriter, r *http.Request) {
	if !consoleWrite(r) || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		respond(w, 403, "forbidden", "Invalid login request", nil)
		return
	}
	s.mu.Lock()
	now := s.now()
	if now.Sub(s.window) >= time.Minute {
		s.window = now
		s.attempts = 0
	}
	if s.attempts >= 10 {
		s.mu.Unlock()
		w.Header().Set("Retry-After", "60")
		respond(w, 429, "rate_limited", "Please wait before trying again", nil)
		return
	}
	s.attempts++
	s.mu.Unlock()
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || len(input.Password) > 1024 {
		respond(w, 400, "invalid_argument", "Invalid login request", nil)
		return
	}
	digest, err := pbkdf2.Key(sha256.New, input.Password, s.salt, 600000, 32)
	name := sha256.Sum256([]byte(input.Username))
	if err != nil || (subtle.ConstantTimeCompare(name[:], s.username[:])&subtle.ConstantTimeCompare(digest, s.password)) != 1 {
		respond(w, 401, "invalid_credentials", "Invalid username or password", nil)
		return
	}
	var entropy [64]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		respond(w, 503, "unavailable", "Login unavailable", nil)
		return
	}
	token, csrf := base64.RawURLEncoding.EncodeToString(entropy[:32]), base64.RawURLEncoding.EncodeToString(entropy[32:])
	s.mu.Lock()
	for key, v := range s.sessions {
		if !now.Before(v.expires) {
			delete(s.sessions, key)
		}
	}
	for _, c := range r.CookiesNamed(SessionCookie) {
		delete(s.sessions, sha256.Sum256([]byte(c.Value)))
	}
	if len(s.sessions) >= 64 {
		s.mu.Unlock()
		respond(w, 429, "rate_limited", "Session capacity reached", nil)
		return
	}
	s.sessions[sha256.Sum256([]byte(token))] = session{expires: now.Add(sessionTTL), csrf: csrf}
	s.mu.Unlock()
	s.setCookie(w, token, int(sessionTTL.Seconds()))
	respond(w, 200, "ok", "", map[string]string{"operator": s.operator, "csrf": csrf})
}

func (s *Sessions) Current(w http.ResponseWriter, r *http.Request) {
	v, ok := s.lookup(r)
	if !ok {
		respond(w, 401, "unauthenticated", "Login required", nil)
		return
	}
	respond(w, 200, "ok", "", map[string]string{"operator": s.operator, "csrf": v.csrf})
}

func (s *Sessions) Logout(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	s.mu.Lock()
	for _, c := range r.CookiesNamed(SessionCookie) {
		delete(s.sessions, sha256.Sum256([]byte(c.Value)))
	}
	s.mu.Unlock()
	s.setCookie(w, "", -1)
	respond(w, 200, "ok", "", nil)
}
