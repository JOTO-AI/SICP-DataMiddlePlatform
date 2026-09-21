package transport

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

type operationKey struct{}

func Operation(ctx context.Context) contract.Operation {
	v, _ := ctx.Value(operationKey{}).(contract.Operation)
	return v
}

// Registry is assembled before serving. It is immutable after Handler is used.
type Registry struct {
	mux        *http.ServeMux
	operations map[contract.Operation]bool
	paths      map[string]bool
	enabled    map[contract.Operation]bool
}

func NewRegistry() *Registry {
	return &Registry{mux: http.NewServeMux(), operations: map[contract.Operation]bool{}, paths: map[string]bool{}, enabled: map[contract.Operation]bool{}}
}
func (r *Registry) Register(d contract.Definition, h http.Handler) error {
	if d.Operation == "" || !strings.HasPrefix(d.Path, "/api/v1/sicp/") || strings.ContainsAny(d.Path, "{}?#%\\") || strings.IndexFunc(d.Path, func(r rune) bool { return r <= ' ' || r == 127 }) >= 0 || path.Clean(d.Path) != d.Path || strings.HasSuffix(d.Path, "/") || h == nil {
		return fmt.Errorf("invalid query definition")
	}
	if r.operations[d.Operation] || r.paths[d.Path] {
		return fmt.Errorf("duplicate query operation or path")
	}
	r.mux.Handle("POST "+d.Path, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		h.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), operationKey{}, d.Operation)))
	}))
	r.operations[d.Operation], r.paths[d.Path] = true, true
	return nil
}
func (r *Registry) Handler() http.Handler { return r.mux }

func (r *Registry) Ready() bool {
	for _, d := range contract.Definitions() {
		if !r.enabled[d.Operation] {
			return false
		}
	}
	return true
}

// Enable is the production gate. Register alone permits isolated mock routing
// tests without claiming a business implementation or completed contract.
func (r *Registry) Enable(d contract.Definition, h http.Handler) error {
	// Consult the authoritative catalog too: clearing a caller's copied list
	// must not bypass unresolved rules on a known operation.
	for _, known := range contract.Definitions() {
		if known.Operation == d.Operation && len(known.PendingRules) != 0 {
			return fmt.Errorf("%s: %s (unresolved contract)", d.Operation, contract.NotReady)
		}
	}
	if len(d.PendingRules) != 0 {
		return fmt.Errorf("%s: %s (unresolved contract)", d.Operation, contract.NotReady)
	}
	if err := r.Register(d, h); err != nil {
		return err
	}
	r.enabled[d.Operation] = true
	return nil
}
