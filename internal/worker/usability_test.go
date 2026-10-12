package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func TestBT01_02ProbeReleasesOnlyItsOwnReceipts(t *testing.T) {
	for _, fail := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(transport.CombinationHeader, "new")
			w.Header().Set(snapshot.Header, strings.Repeat("a", 64))
			fmt.Fprint(w, `{"code":"ok","data":{}}`)
		}))
		releases := 0
		err := probe(t.Context(), server.URL, "key", "new", func(_ context.Context, token, bundle, op, conditions string) error {
			releases++
			if token != strings.Repeat("a", 64) || bundle != "new" || (op != string(contract.StudentGrades) && op != string(contract.BatchGrades)) || !strings.Contains(conditions, "__build_probe__") {
				t.Error("unbound snapshot release")
			}
			if fail {
				return errors.New("release failed")
			}
			return nil
		})
		server.Close()
		if fail {
			if err == nil || releases != 1 {
				t.Fatal("receipt release failure hidden")
			}
		} else if err != nil || releases != 2 {
			t.Fatal("probe retained its own snapshots")
		}
	}
}

func TestBT01_02UsabilityRequiresAllAuthenticatedNewCombinationResponses(t *testing.T) {
	for _, mode := range []string{"success", "old", "missing", "unauthorized", "invalid", "redirect", "last-fails", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			var visits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				visits.Add(1)
				if r.Header.Get("X-API-Key") != "test-key" || r.Method != "POST" {
					t.Error("ordinary auth required")
				}
				matched := false
				for _, d := range contract.LegacyBusinessDefinitions() {
					if r.URL.Path == d.Path {
						matched = true
						if _, err := transport.Parse(d.Operation, r.Body); err != nil {
							t.Error("probe violates production request contract")
						}
					}
				}
				if !matched {
					t.Error("unknown operation")
				}
				if mode == "redirect" {
					http.Redirect(w, r, "/elsewhere", 302)
					return
				}
				bundle := "new"
				if mode == "old" {
					bundle = "old"
				}
				if mode != "missing" {
					w.Header().Set(transport.CombinationHeader, bundle)
				}
				if mode == "unauthorized" || mode == "last-fails" && visits.Load() == 4 {
					w.WriteHeader(401)
					return
				}
				if mode == "invalid" {
					fmt.Fprint(w, `{"code":"ok","data":null}`)
					return
				}
				fmt.Fprint(w, `{"code":"ok","data":{}}`)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			err := Probe(ctx, server.URL, "test-key", "new")
			if mode == "success" {
				if err != nil || visits.Load() != 4 {
					t.Fatalf("visits=%d error=%v", visits.Load(), err)
				}
			} else if err == nil {
				t.Fatal("false usable evidence")
			}
			if mode == "redirect" && visits.Load() != 1 {
				t.Fatal("credentials followed redirect")
			}
		})
	}
}
