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
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/staff"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func staffRepo() *repository {
	return &repository{tables: map[string][]source.SourceRow{
		staff.Table:         {sourceRow(`{"JGH":"01","XM":"A%_甲","DQZTM":"11","DWH":"D1","ETL_TIME":"2026-09-18 08:00:00"}`), sourceRow(`{"JGH":"001","XM":"a乙","DQZTM":"unknown","DWH":"missing"}`), sourceRow(`{"JGH":"1","XM":"A丙","DQZTM":"04","DWH":"D1"}`)},
		staff.ContactsTable: {sourceRow(`{"JGH":"01","DZXX":"raw invalid","YDDH":"same","ETL_TIME":"2026-09-18 08:30:00"}`), sourceRow(`{"JGH":"1","YDDH":"same"}`), sourceRow(`{"JGH":"orphan","ETL_TIME":"bad"}`)},
		organizations.Table: {sourceRow(`{"DWH":"D1","DWMC":"A%_院","DWYXBSM":"0","SFSC":"0","ETL_TIME":"2026-09-18 08:20:00"}`), sourceRow(`{"DWH":"unrelated","DWMC":123,"ETL_TIME":"bad"}`)},
	}}
}
func staffHandler(t *testing.T, repo *repository, limits read.Limits) (http.Handler, *admission) {
	t.Helper()
	clock := testkit.FakeClock{Time: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}
	recorder, err := events.New(clock, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	times, _ := compute.NewTimes("Asia/Shanghai")
	a := &admission{}
	h, err := handlers.Staff("synthetic-key", staff.Service{Source: repo, Clock: clock, Times: times, Limits: limits}, a, metadata{operation: contract.Staff}, recorder, func(*events.Span, error) {})
	if err != nil {
		t.Fatal(err)
	}
	return h, a
}
func staffRequest(h http.Handler, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/sicp/staff/query", strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestT19_04StaffFilterIntersection(t *testing.T) {
	for _, tc := range []struct {
		body string
		ids  []string
	}{
		{`{}`, []string{"001", "01", "1"}},
		{`{"name":"A"}`, []string{"01", "1"}},
		{`{"name":"%_"}`, []string{"01"}},
		{`{"department_code":"D1"}`, []string{"01", "1"}},
		{`{"department_code":"missing"}`, []string{"001"}},
		{`{"department_name":"%_"}`, []string{"01", "1"}},
		{`{"status_codes":["unknown","04","04"]}`, []string{"001", "1"}},
		{`{"staff_number":"01","name":"A","department_code":"D1","department_name":"%_","status_codes":["11","04"]}`, []string{"01"}},
		{`{"staff_number":"01","status_codes":["unknown"]}`, []string{}},
		{`{"staff_number":"orphan"}`, []string{}},
	} {
		repo := staffRepo()
		h, a := staffHandler(t, repo, read.DefaultLimits())
		w := staffRequest(h, tc.body, "synthetic-key")
		var got struct{ Data contract.StaffResponse }
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		ids := []string{}
		for _, item := range got.Data.Items {
			ids = append(ids, item.StaffNumber)
		}
		if w.Code != 200 || got.Data.Total != len(tc.ids) || !reflect.DeepEqual(ids, tc.ids) {
			t.Fatal(tc.body, w.Code, w.Body.String())
		}
		if a.calls != 1 || a.closed != 1 {
			t.Fatal(a)
		}
		for _, call := range repo.calls {
			if call.h != pinned {
				t.Fatal(call)
			}
		}
		if len(repo.calls) > 3 {
			t.Fatal("unbounded per-person reads", repo.calls)
		}
	}
}

func TestT19_05StaffPaginationContactsAndTimestamps(t *testing.T) {
	repo := staffRepo()
	h, _ := staffHandler(t, repo, read.DefaultLimits())
	w := staffRequest(h, `{"page":2,"page_size":1}`, "synthetic-key")
	var envelope struct{ Data json.RawMessage }
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	var got, want any
	_ = json.Unmarshal(envelope.Data, &got)
	_ = json.Unmarshal([]byte(`{"total":3,"synced_at":"2026-09-18T09:00:00+08:00","items":[{"staff_number":"01","name":"A%_甲","status_code":"11","department":{"organization_code":"D1","organization_name":"A%_院","parent_organization_code":null,"category_code":null,"category_name":null,"is_active":false},"email":"raw invalid","mobile":"same","source_updated_at":"2026-09-18T08:30:00+08:00"}]}`), &want)
	if w.Code != 200 || !reflect.DeepEqual(got, want) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = staffRequest(h, `{}`, "synthetic-key")
	var all struct{ Data contract.StaffResponse }
	_ = json.Unmarshal(w.Body.Bytes(), &all)
	if w.Code != 200 || all.Data.Total != 3 || len(all.Data.Items) != 3 || *all.Data.Items[1].Mobile != *all.Data.Items[2].Mobile || all.Data.Items[0].Department.IsActive != nil {
		t.Fatal(w.Body.String())
	}
	w = staffRequest(h, `{"page":1000000,"page_size":1000}`, "synthetic-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":3`) || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Body.String())
	}
	large := &repository{tables: map[string][]source.SourceRow{}}
	for i := 0; i < 1001; i++ {
		large.tables[staff.Table] = append(large.tables[staff.Table], sourceRow(fmt.Sprintf(`{"JGH":"%04d","XM":"N"}`, i)))
		large.tables[staff.ContactsTable] = append(large.tables[staff.ContactsTable], sourceRow(fmt.Sprintf(`{"JGH":"%04d","YDDH":"same"}`, i)))
	}
	h, _ = staffHandler(t, large, read.DefaultLimits())
	w = staffRequest(h, `{}`, "synthetic-key")
	_ = json.Unmarshal(w.Body.Bytes(), &all)
	if w.Code != 200 || all.Data.Total != 1001 || len(all.Data.Items) != 1000 || all.Data.Items[999].StaffNumber != "0999" || len(large.calls) != 7 {
		t.Fatal(w.Code, all.Data.Total, len(all.Data.Items), len(large.calls))
	}
}

func TestT19_06StaffFailuresDiscardPartialResults(t *testing.T) {
	for _, kind := range []string{"auth", "parse", "duplicate basic", "duplicate contact", "missing name", "contact SQL", "unit SQL", "late time", "budget", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			repo := staffRepo()
			limits := read.DefaultLimits()
			key := "synthetic-key"
			body := `{}`
			status := 500
			switch kind {
			case "auth":
				key = "bad"
				status = 401
			case "parse":
				body = `{"status_codes":[]}`
				status = 400
			case "duplicate basic":
				repo.tables[staff.Table] = append(repo.tables[staff.Table], repo.tables[staff.Table][0])
			case "duplicate contact":
				repo.tables[staff.ContactsTable] = append(repo.tables[staff.ContactsTable], repo.tables[staff.ContactsTable][0])
			case "missing name":
				repo.tables[staff.Table][0]["XM"] = json.RawMessage(`null`)
			case "contact SQL":
				repo.fail = staff.ContactsTable
				status = 503
			case "unit SQL":
				repo.fail = organizations.Table
				status = 503
			case "late time":
				repo.tables[staff.ContactsTable][1]["ETL_TIME"] = json.RawMessage(`"bad"`)
			case "budget":
				limits.Rows = 4
				status = 422
			case "cancel":
				status = 504
			}
			h, a := staffHandler(t, repo, limits)
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
