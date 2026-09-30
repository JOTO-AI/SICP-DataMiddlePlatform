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

type tableAdmissionSpy struct {
	admissionSpy
	request control.Request
}

func (s *tableAdmissionSpy) AdmitTable(_ context.Context, r control.Request) (control.Acceptance, error) {
	s.request = r
	return control.Acceptance{Task: control.Task{ID: r.ID, Status: "accepted", AcceptedAt: r.AcceptedAt}, Created: true}, nil
}
func TestTableForceFullRouteUsesTrustedIdentityAndForceFullPermission(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		spy := &tableAdmissionSpy{}
		a := admin.Admission{Store: spy, Clock: testkit.FakeClock{Time: time.Now()}, Random: testkit.FixedRandom(1)}
		action := admin.Status
		if allowed {
			action = admin.ForceFull
		}
		mux, err := admin.Routes(administrator(t, action), map[admin.Action]http.Handler{admin.Incremental: a, admin.ForceFull: a, admin.Rerun: a, admin.Status: http.NotFoundHandler()})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/admin/v1/tables/dwd_bzkscjzlb/force-full", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Idempotency-Key", "table-repair")
		req.Header.Set("X-Operator", "forged")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if allowed {
			if w.Code != 202 || spy.request.Table != "dwd_bzkscjzlb" || spy.request.RequestedBy != "operator-001" || spy.request.IdempotencyKey != "table-repair" {
				t.Fatal(w.Code, spy.request)
			}
		} else if w.Code != 403 || spy.request.ID != "" {
			t.Fatal("unauthorized table update", w.Code)
		}
		if spy.calls != 0 {
			t.Fatal("single-table route started whole update")
		}
	}
}
func TestT15_02MalformedAdmissionNeverReachesControlTransaction(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"requested_by":"forged"}`, `{} {}`, strings.Repeat(" ", 4100) + `{}`} {
		t.Run(body[:min(len(body), 20)], func(t *testing.T) {
			spy := &admissionSpy{}
			a := admin.Admission{Store: spy, Clock: testkit.FakeClock{Time: time.Now()}, Random: testkit.FixedRandom(1)}
			h, err := administrator(t, admin.ForceFull).Protect(admin.ForceFull, a)
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
