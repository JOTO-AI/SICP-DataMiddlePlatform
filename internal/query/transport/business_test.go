package transport_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func TestT07_17BusinessMapsLimitsTimeoutAndDependencyErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{read.ErrBudget, 422, "resource_exhausted"}, {read.ErrHandle, 503, "not_ready"}, {context.DeadlineExceeded, 504, "deadline_exceeded"}, {errors.New("private SQL"), 503, "dependency_failure"}} {
		recorder, _ := events.New(testkit.FakeClock{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second)
		h, err := transport.Business(contract.StudentGrades, "synthetic-key", recorder, func(*events.Span, error) {}, func(context.Context, any) (any, error) { return "partial", tc.err })
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"student_number":"001"}`))
		r.Header.Set("X-API-Key", "synthetic-key")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) || !strings.Contains(w.Body.String(), `"data":null`) || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "partial") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
