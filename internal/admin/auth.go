// Package admin exposes independently authorized management operations.
package admin

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type Action string

const (
	Incremental Action = "incremental"
	ForceFull   Action = "force_full"
	Rerun       Action = "rerun"
	Status      Action = "status"
)

func validAction(a Action) bool {
	return a == Incremental || a == ForceFull || a == Rerun || a == Status
}

type Credential struct {
	Operator string   `json:"operator"`
	Token    string   `json:"token"`
	Actions  []Action `json:"actions"`
}
type grant struct {
	operator string
	digest   [32]byte
	actions  map[Action]bool
}
type Auth struct{ grants []grant }
type identityKey struct{}
type identity struct {
	operator string
	action   Action
}

func Identity(ctx context.Context) (string, Action, bool) {
	v, ok := ctx.Value(identityKey{}).(identity)
	return v.operator, v.action, ok
}
func validToken(s string) bool {
	if len(s) < 16 || len(s) > 4096 {
		return false
	}
	for _, r := range s {
		if r < 33 || r > 126 || r == ',' {
			return false
		}
	}
	return true
}

// NewAuth snapshots permissions and retains only token digests. Query service
// credentials are forbidden as admin tokens, even when put in a Bearer header.
func NewAuth(queryKey string, credentials []Credential) (*Auth, error) {
	if queryKey == "" || len(credentials) == 0 || len(credentials) > 128 {
		return nil, errors.New("management credentials required")
	}
	out := &Auth{}
	operators := map[string]bool{}
	digests := map[[32]byte]bool{}
	query := sha256.Sum256([]byte(queryKey))
	for _, c := range credentials {
		if strings.TrimSpace(c.Operator) != c.Operator || c.Operator == "" || len(c.Operator) > 128 || strings.IndexFunc(c.Operator, func(r rune) bool { return r < 32 || r == 127 }) >= 0 || !validToken(c.Token) || len(c.Actions) == 0 {
			return nil, errors.New("invalid management credential")
		}
		digest := sha256.Sum256([]byte(c.Token))
		if digest == query || digests[digest] || operators[c.Operator] {
			return nil, errors.New("management credentials must be independent and unique")
		}
		g := grant{operator: c.Operator, digest: digest, actions: map[Action]bool{}}
		for _, a := range c.Actions {
			if !validAction(a) || g.actions[a] {
				return nil, errors.New("invalid management permission")
			}
			g.actions[a] = true
		}
		out.grants = append(out.grants, g)
		digests[digest] = true
		operators[c.Operator] = true
	}
	return out, nil
}

func (a *Auth) Protect(action Action, next http.Handler) (http.Handler, error) {
	if a == nil || len(a.grants) == 0 || !validAction(action) || next == nil {
		return nil, errors.New("management authorization not configured")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var values []string
		for name, v := range r.Header {
			if strings.EqualFold(name, "Authorization") {
				values = append(values, v...)
			}
		}
		if len(values) != 1 {
			respond(w, 401, "unauthenticated", "Management authentication required", nil)
			return
		}
		scheme, token, ok := strings.Cut(values[0], " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || !validToken(token) {
			respond(w, 401, "unauthenticated", "Management authentication required", nil)
			return
		}
		digest := sha256.Sum256([]byte(token))
		matched := -1
		for i, g := range a.grants {
			if subtle.ConstantTimeCompare(digest[:], g.digest[:]) == 1 {
				matched = i
			}
		}
		if matched < 0 {
			respond(w, 401, "unauthenticated", "Management authentication required", nil)
			return
		}
		g := a.grants[matched]
		if !g.actions[action] {
			respond(w, 403, "forbidden", "Action not permitted", nil)
			return
		}
		ctx := context.WithValue(r.Context(), identityKey{}, identity{operator: g.operator, action: action})
		next.ServeHTTP(w, r.WithContext(ctx))
	}), nil
}

func respond(w http.ResponseWriter, status int, code, message string, data any) {
	body := struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	}{code, message, data}
	raw, err := json.Marshal(body)
	if err != nil {
		status = 500
		raw = []byte(`{"code":"internal","message":"Internal error","data":null}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}
