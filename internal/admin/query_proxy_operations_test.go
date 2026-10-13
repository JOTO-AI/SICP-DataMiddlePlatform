package admin

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestT32_16And17NineRoutesPreserveBusinessResponseAndSnapshot(t *testing.T) {
	paths := []string{"students/grades/query", "students/grades/batch-query", "students/timetable/query", "staff/timetable/query", "organizations/query", "students/query", "staff/query", "staff/teaching-classes/query", "teaching-classes/students/query"}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-API-Key") != "server-key" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials not isolated")
		}
		if r.Header.Get("X-Query-Snapshot") != "fixed-bundle" {
			t.Error("snapshot not forwarded")
		}
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != `{"page":2}` {
			t.Error("request rewritten")
		}
		w.Header().Set("X-Request-ID", "upstream-request")
		w.Header().Set("X-Query-Snapshot", "fixed-bundle")
		w.Header().Set("X-API-Key", "must-not-leak")
		w.WriteHeader(409)
		fmt.Fprint(w, `{"code":"conflict","data":null}`)
	}))
	defer upstream.Close()
	h, err := QueryProxy(upstream.URL, "server-key")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		r := httptest.NewRequest("POST", "/console-api/query/api/v1/sicp/"+path, strings.NewReader(`{"page":2}`))
		r.Header.Set("X-Query-Snapshot", "fixed-bundle")
		r.Header.Set("Cookie", "browser-secret")
		r.Header.Set("Authorization", "browser-secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 409 || w.Body.String() != `{"code":"conflict","data":null}` || w.Header().Get("X-Request-ID") != "upstream-request" || w.Header().Get("X-Query-Snapshot") != "fixed-bundle" || w.Header().Get("X-API-Key") != "" {
			t.Fatal(path, w.Code, w.Header())
		}
	}
	if calls != 9 {
		t.Fatal("not all nine routes reached the existing service")
	}
}
