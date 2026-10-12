package transport_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT17_06SnapshotHTTPValidationAndAuthOrder(t *testing.T) {
	validToken := strings.Repeat("a", 64)
	cases := phase2ParseCases()
	cases = append(cases,
		phase2ParseCase{op: contract.StudentGrades, base: `{"student_number":"001"}`},
		phase2ParseCase{op: contract.BatchGrades, base: `{"academic_year":"all"}`},
	)
	for _, tc := range cases {
		t.Run(string(tc.op), func(t *testing.T) {
			recorder, err := events.New(testkit.FakeClock{Time: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)}, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			var observed string
			var runErr error
			h, err := transport.BusinessWithSnapshot(tc.op, "synthetic-key", recorder, func(*events.Span, error) {}, func(ctx context.Context, _ any) (any, error) {
				calls++
				observed = transport.SnapshotToken(ctx)
				return transport.Reply{Data: map[string]any{"total": 0, "items": []any{}}, Snapshot: validToken, Combination: "C1"}, runErr
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range []struct {
				key    string
				values []string
				status int
			}{
				{"wrong", []string{"bad"}, 401}, {"synthetic-key", []string{"bad"}, 409}, {"synthetic-key", []string{""}, 409}, {"synthetic-key", []string{validToken, validToken}, 409}, {"synthetic-key", nil, 200}, {"synthetic-key", []string{validToken}, 200},
			} {
				before := calls
				r := httptest.NewRequest("POST", "/", strings.NewReader(tc.base))
				r.Header.Set("X-API-Key", input.key)
				if input.values != nil {
					r.Header[snapshot.Header] = input.values
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != input.status {
					t.Fatal(w.Code, w.Body.String())
				}
				if input.status != 200 {
					if calls != before || w.Header().Get(snapshot.Header) != "" || !strings.Contains(w.Body.String(), `"data":null`) {
						t.Fatal("invalid request entered use case")
					}
				} else {
					want := ""
					if input.values != nil {
						want = validToken
					}
					if calls != before+1 || observed != want || w.Header().Get(snapshot.Header) != validToken || w.Header().Get(transport.CombinationHeader) != "C1" {
						t.Fatal("snapshot metadata lost")
					}
				}
			}
			runErr = snapshot.ErrSnapshot
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.base))
			r.Header.Set("X-API-Key", "synthetic-key")
			r.Header.Set(snapshot.Header, validToken)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 409 || w.Header().Get(snapshot.Header) != "" || w.Header().Get(transport.CombinationHeader) != "" || !strings.Contains(w.Body.String(), `"data":null`) {
				t.Fatal("snapshot failure became partial success", w.Code, w.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{
		{contract.StudentTimetable, `{"student_number":"001","academic_year":"2026-2027","semester":"1"}`},
		{contract.StaffTimetable, `{"staff_number":"001","academic_year":"2026-2027","semester":"1"}`},
	} {
		recorder, err := events.New(testkit.FakeClock{Time: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)}, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		h, err := transport.BusinessWithSnapshot(tc.op, "synthetic-key", recorder, func(*events.Span, error) {}, func(context.Context, any) (any, error) { t.Fatal("timetable accepted snapshot"); return nil, nil })
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		r.Header.Set("X-API-Key", "synthetic-key")
		r.Header.Set(snapshot.Header, validToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if contract.SupportsSnapshot("unknown") || contract.SupportsSnapshot(contract.StudentTimetable) || contract.SupportsSnapshot(contract.StaffTimetable) {
		t.Fatal("unexpected snapshot capability")
	}
}
