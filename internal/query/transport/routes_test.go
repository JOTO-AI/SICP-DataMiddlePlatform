package transport_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func TestT01_01RoutesAndOperationsAreUnique(t *testing.T) {
	r := transport.NewRegistry()
	definitions := contract.Definitions()
	for _, d := range definitions {
		h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if got := transport.Operation(req.Context()); got != d.Operation {
				t.Errorf("operation got %s want %s", got, d.Operation)
			}
			w.WriteHeader(http.StatusNoContent)
		})
		if err := r.Register(d, h); err != nil {
			t.Fatal(err)
		}
		if err := r.Register(d, h); err == nil {
			t.Fatal("duplicate accepted")
		}
		other := d
		other.Operation = "duplicate.path"
		if err := r.Register(other, h); err == nil {
			t.Fatal("duplicate path accepted")
		}
		other = d
		other.Path = "/api/v1/sicp/other"
		if err := r.Register(other, h); err == nil {
			t.Fatal("duplicate operation accepted")
		}
	}
	for _, d := range definitions {
		rec := httptest.NewRecorder()
		r.Handler().ServeHTTP(rec, httptest.NewRequest("POST", d.Path, nil))
		if rec.Code != 204 {
			t.Fatalf("%s: %d", d.Path, rec.Code)
		}
		rec = httptest.NewRecorder()
		r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", d.Path, nil))
		if rec.Code != 405 || rec.Header().Get("Allow") != "POST" {
			t.Fatal("incorrect method allowed")
		}
	}
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/sicp/unknown", nil))
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
}

func TestT01_09UnresolvedContractCannotBeEnabled(t *testing.T) {
	for _, d := range contract.Definitions() {
		if len(d.PendingRules) == 0 {
			continue
		}
		r := transport.NewRegistry()
		err := r.Enable(d, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("pending business executed") }))
		if err == nil || !strings.Contains(err.Error(), string(contract.NotReady)) {
			t.Fatal(err)
		}
		d.PendingRules = nil
		if err = r.Enable(d, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); err == nil {
			t.Fatal("caller bypassed authoritative pending rules")
		}
		rec := httptest.NewRecorder()
		r.Handler().ServeHTTP(rec, httptest.NewRequest("POST", d.Path, nil))
		if rec.Code != 404 {
			t.Fatal("failed enable registered a route")
		}
	}
}

func TestInvalidRoutePatternsAreRejected(t *testing.T) {
	for _, path := range []string{"", "/wrong", "/api/v1/sicp/{wild}", "/api/v1/sicp/a/../b", "/api/v1/sicp/a//b", "/api/v1/sicp/a%2Fb", "/api/v1/sicp/a\r"} {
		d := contract.Definitions()[0]
		d.Path = path
		if err := transport.NewRegistry().Register(d, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); err == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
}
