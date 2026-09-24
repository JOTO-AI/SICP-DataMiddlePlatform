package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type admissionSpy struct{ calls int }

func (s *admissionSpy) Admit(context.Context, control.Request) (control.Acceptance, error) {
	s.calls++
	return control.Acceptance{}, nil
}
func TestT15_02MalformedAdmissionNeverReachesControlTransaction(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"requested_by":"forged"}`, `{} {}`, strings.Repeat(" ", 4100) + `{}`} {
		t.Run(body[:min(len(body), 20)], func(t *testing.T) {
			spy := &admissionSpy{}
			a := admin.Admission{Store: spy, Clock: testkit.FakeClock{Time: time.Now()}, Random: testkit.FixedRandom(1)}
			h, err := administrator(t, admin.Initialize).Protect(admin.Initialize, a)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+adminToken)
			req.Header.Set("Idempotency-Key", "test-request")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest || spy.calls != 0 {
				t.Fatal(w.Code, spy.calls)
			}
		})
	}
}
