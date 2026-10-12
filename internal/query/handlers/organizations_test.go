package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/handlers"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func organizationRows() []source.SourceRow {
	return []source.SourceRow{
		sourceRow(`{"DWH":"10001","DWMC":"学校","LSDWH":"ignored","DWLBM":"5","DWYXBSM":"1","SFSC":"0","ETL_TIME":"2026-09-18 08:00:00"}`),
		sourceRow(`{"DWH":"01","DWMC":"A%_院","LSDWH":"10001","DWLBM":"1","DWYXBSM":"1","SFSC":"0","ETL_TIME":"2026-09-18 08:10:00"}`),
		sourceRow(`{"DWH":"1","DWMC":"A院","LSDWH":"10001","DWLBM":"5","DWYXBSM":"0","SFSC":"0"}`),
		sourceRow(`{"DWH":"001","DWMC":"a院","LSDWH":"01","DWLBM":"unknown"}`),
	}
}

func organizationHandler(t *testing.T, repo *repository, limits read.Limits) (http.Handler, *admission) {
	t.Helper()
	clock := testkit.FakeClock{Time: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}
	recorder, err := events.New(clock, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	times, _ := compute.NewTimes("Asia/Shanghai")
	a := &admission{}
	h, err := handlers.Organizations("synthetic-key", organizations.Service{Source: repo, Times: times, Clock: clock, Limits: limits}, a, metadata{operation: contract.Organizations}, recorder, func(*events.Span, error) {})
	if err != nil {
		t.Fatal(err)
	}
	return h, a
}

func organizationRequest(h http.Handler, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/sicp/organizations/query", strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestT18_04OrganizationFilterIntersection(t *testing.T) {
	for _, tc := range []struct {
		body string
		ids  []string
	}{
		{`{}`, []string{"001", "01", "1", "10001"}},
		{`{"parent_organization_code":"10001"}`, []string{"01", "1"}},
		{`{"parent_organization_code":"01"}`, []string{"001"}},
		{`{"organization_name":"%_"}`, []string{"01"}},
		{`{"organization_name":"A"}`, []string{"01", "1"}},
		{`{"category_codes":["unknown","5","5"]}`, []string{"001", "1", "10001"}},
		{`{"is_active":false}`, []string{"001", "1"}},
		{`{"organization_code":"01","organization_name":"A","parent_organization_code":"10001","category_codes":["1","5"],"is_active":true}`, []string{"01"}},
		{`{"organization_code":"01","is_active":false}`, []string{}},
		{`{"parent_organization_code":"ignored"}`, []string{}},
	} {
		repo := &repository{tables: map[string][]source.SourceRow{organizations.Table: organizationRows()}}
		h, a := organizationHandler(t, repo, read.DefaultLimits())
		w := organizationRequest(h, tc.body, "synthetic-key")
		var body struct {
			Data contract.OrganizationsResponse
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, item := range body.Data.Items {
			ids = append(ids, item.OrganizationCode)
		}
		if w.Code != 200 || !reflect.DeepEqual(ids, tc.ids) || body.Data.Total != len(tc.ids) || body.Data.SyncedAt != "2026-09-18T09:00:00+08:00" {
			t.Fatal(tc.body, w.Code, w.Body.String())
		}
		if a.calls != 1 || a.closed != 1 || len(repo.calls) != 1 || len(repo.calls[0].filters) != 0 || repo.calls[0].h != pinned {
			t.Fatal(a, repo.calls)
		}
	}
}

func TestT18_05OrganizationPaginationAndWholeResponse(t *testing.T) {
	for _, tc := range []struct {
		body string
		ids  []string
	}{
		{`{"page":2,"page_size":2}`, []string{"1", "10001"}},
		{`{"page":3,"page_size":2}`, []string{}},
		{`{"page":1000000,"page_size":1000}`, []string{}},
	} {
		repo := &repository{tables: map[string][]source.SourceRow{organizations.Table: organizationRows()}}
		limits := read.DefaultLimits()
		limits.BatchSize = 2
		h, _ := organizationHandler(t, repo, limits)
		w := organizationRequest(h, tc.body, "synthetic-key")
		var body struct {
			Data contract.OrganizationsResponse
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		ids := []string{}
		for _, item := range body.Data.Items {
			ids = append(ids, item.OrganizationCode)
		}
		if w.Code != 200 || body.Data.Total != 4 || !reflect.DeepEqual(ids, tc.ids) || len(repo.calls) != 3 {
			t.Fatal(w.Code, w.Body.String(), repo.calls)
		}
		if len(tc.ids) == 0 && !strings.Contains(w.Body.String(), `"items":[]`) {
			t.Fatal(w.Body.String())
		}
	}
	repo := &repository{tables: map[string][]source.SourceRow{organizations.Table: organizationRows()}}
	h, _ := organizationHandler(t, repo, read.DefaultLimits())
	w := organizationRequest(h, `{"organization_code":"01"}`, "synthetic-key")
	var got struct{ Data json.RawMessage }
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	var actual, want any
	_ = json.Unmarshal(got.Data, &actual)
	_ = json.Unmarshal([]byte(`{"total":1,"synced_at":"2026-09-18T09:00:00+08:00","items":[{"organization_code":"01","organization_name":"A%_院","parent_organization_code":"10001","category_code":"1","category_name":"教学院系","is_active":true,"source_updated_at":"2026-09-18T08:10:00+08:00"}]}`), &want)
	if !reflect.DeepEqual(actual, want) {
		t.Fatal(w.Body.String())
	}
	large := []source.SourceRow{organizationRows()[0]}
	for i := 0; i < 1000; i++ {
		large = append(large, sourceRow(fmt.Sprintf(`{"DWH":"D%04d","DWMC":"N","LSDWH":"10001"}`, i)))
	}
	repo = &repository{tables: map[string][]source.SourceRow{organizations.Table: large}}
	h, _ = organizationHandler(t, repo, read.DefaultLimits())
	w = organizationRequest(h, `{}`, "synthetic-key")
	var largeBody struct {
		Data contract.OrganizationsResponse
	}
	_ = json.Unmarshal(w.Body.Bytes(), &largeBody)
	if w.Code != 200 || largeBody.Data.Total != 1001 || len(largeBody.Data.Items) != 1000 || largeBody.Data.Items[999].OrganizationCode != "D0998" {
		t.Fatal(w.Code, largeBody.Data.Total, len(largeBody.Data.Items))
	}
}

func TestT18_06OrganizationAuthFaultsAndHiddenInvalidGraph(t *testing.T) {
	for _, kind := range []string{"auth", "parse", "sql", "late sql", "graph", "budget", "cancel", "duplicate", "empty"} {
		t.Run(kind, func(t *testing.T) {
			repo := &repository{tables: map[string][]source.SourceRow{organizations.Table: organizationRows()}}
			limits := read.DefaultLimits()
			key := "synthetic-key"
			body := `{"organization_code":"10001"}`
			status := 500
			switch kind {
			case "auth":
				key = "bad"
				body = "{"
				status = 401
			case "parse":
				body = `{"page":0}`
				status = 400
			case "sql":
				repo.fail = organizations.Table
				status = 503
			case "late sql":
				limits.BatchSize = 2
				repo.failAt = 2
				status = 503
			case "graph":
				repo.tables[organizations.Table][3]["LSDWH"] = json.RawMessage(`"missing"`)
			case "budget":
				limits.Rows = 2
				status = 422
			case "duplicate":
				repo.tables[organizations.Table] = append(repo.tables[organizations.Table], repo.tables[organizations.Table][0])
			case "empty":
				repo.tables[organizations.Table] = nil
			case "cancel":
				status = 504
			}
			h, a := organizationHandler(t, repo, limits)
			r := httptest.NewRequest("POST", "/", strings.NewReader(body))
			r.Header.Set("X-API-Key", key)
			if kind == "cancel" {
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				repo.cancel = cancel
				r = r.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != status || !strings.Contains(w.Body.String(), `"data":null`) || strings.Contains(w.Body.String(), "private SQL") {
				t.Fatal(w.Code, w.Body.String())
			}
			if kind == "auth" || kind == "parse" {
				if a.calls != 0 || len(repo.calls) != 0 {
					t.Fatal(a, repo.calls)
				}
			} else if a.calls != 1 || a.closed != 1 {
				t.Fatal(a)
			}
		})
	}
}
