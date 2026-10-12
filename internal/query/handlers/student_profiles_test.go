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
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/students"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func studentProfilesRepo() *repository {
	return &repository{tables: map[string][]source.SourceRow{
		students.Table:            {sourceRow(`{"XH":"01","XM":"学生","ETL_TIME":"2026-09-18 08:00:00"}`), sourceRow(`{"XH":"001","XM":"乙"}`), sourceRow(`{"XH":"1","XM":"丙"}`)},
		students.EnrollmentsTable: {sourceRow(`{"XH":"01","YXSH":"D1","SSXQH":"X1","ZYH":"Z1","SZBH":"B1","SZNJ":"2026","XSDQZTM":"raw","XJZTM":"enrolled","SFZX":"0","XSLBM":"432","XZ":"3.0","RXNY":"202609","YJBYRQ":"20290630"}`), sourceRow(`{"XH":"001"}`), sourceRow(`{"XH":"1"}`)},
		students.ContactsTable:    {sourceRow(`{"XH":"01","DZXX":"invalid email","YDDH":"shared","ETL_TIME":"2026-09-18 08:30:00"}`), sourceRow(`{"XH":"001","YDDH":"shared"}`), sourceRow(`{"XH":"orphan","ETL_TIME":"bad"}`)},
		organizations.Table:       {sourceRow(`{"DWH":"D1","DWMC":"院","DWYXBSM":"1","SFSC":"0"}`)},
		students.CampusesTable:    {sourceRow(`{"XQH":"X1","XQMC":"校区"}`)},
		students.MajorsTable:      {sourceRow(`{"ZYH":"Z1","ZYMC":"专业"}`)},
		students.ClassesTable:     {sourceRow(`{"BH":"B1","BJMC":"班","FDYJGH":"T1"}`)},
		staff.Table:               {sourceRow(`{"JGH":"T1","XM":"教师","ETL_TIME":"2026-09-18 08:40:00"}`)},
	}}
}
func studentProfilesHandler(t *testing.T, repo *repository, limits read.Limits) (http.Handler, *admission) {
	t.Helper()
	clock := testkit.FakeClock{Time: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}
	recorder, err := events.New(clock, testkit.FixedRandom(1), events.SinkFunc(func(context.Context, events.Event) error { return nil }), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	times, _ := compute.NewTimes("Asia/Shanghai")
	a := &admission{}
	h, err := handlers.Students("synthetic-key", students.Service{Source: repo, Clock: clock, Times: times, Limits: limits}, a, metadata{operation: contract.Students}, recorder, func(*events.Span, error) {})
	if err != nil {
		t.Fatal(err)
	}
	return h, a
}
func studentProfilesRequest(h http.Handler, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/sicp/students/query", strings.NewReader(body))
	r.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestT20_07StudentWholeHTTPResponseAndPinnedPage(t *testing.T) {
	repo := studentProfilesRepo()
	h, a := studentProfilesHandler(t, repo, read.DefaultLimits())
	w := studentProfilesRequest(h, `{"page":2,"page_size":1}`, "synthetic-key")
	var envelope struct{ Data json.RawMessage }
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	var got, want any
	_ = json.Unmarshal(envelope.Data, &got)
	_ = json.Unmarshal([]byte(`{"total":3,"synced_at":"2026-09-18T09:00:00+08:00","items":[{"student_number":"01","name":"学生","status_code":"raw","enrollment_status_code":"enrolled","on_campus_flag":"0","department":{"organization_code":"D1","organization_name":"院","parent_organization_code":null,"category_code":null,"category_name":null,"is_active":true},"campus_code":"X1","campus_name":"校区","major_code":"Z1","major_name":"专业","grade":"2026","class_code":"B1","class_name":"班","student_category_code":"432","student_category_name":"中高贯通生","study_years":3,"admission_month":"2026-09","expected_graduation_date":"2029-06-30","counselor":{"staff_number":"T1","name":"教师"},"email":"invalid email","mobile":"shared","source_updated_at":"2026-09-18T08:40:00+08:00"}]}`), &want)
	if w.Code != 200 || !reflect.DeepEqual(got, want) || a.calls != 1 || a.closed != 1 || len(repo.calls) != 8 {
		t.Fatal(w.Code, w.Body.String(), a, repo.calls)
	}
	for _, call := range repo.calls {
		if call.h != pinned {
			t.Fatal(call)
		}
		if call.table == students.ContactsTable && !reflect.DeepEqual(call.filters, []source.Filter{{Column: "XH", Values: []string{"01"}}}) {
			t.Fatal("off-page contact fetched", call)
		}
	}
	body := `{"student_number":"01","name":"学","department_code":"D1","department_name":"院","campus_code":"X1","major_code":"Z1","grade":"2026","class_code":"B1","status_codes":["raw"],"enrollment_status_codes":["enrolled"],"on_campus_flag":"0","student_category_codes":["432"],"study_years":3,"admission_year":"2026","expected_graduation_year":"2029","counselor_staff_number":"T1"}`
	w = studentProfilesRequest(h, body, "synthetic-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = studentProfilesRequest(h, `{"page":1000000}`, "synthetic-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":3`) || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Body.String())
	}
}

func TestT20_08StudentStrictScopeCannotBeHiddenByFilters(t *testing.T) {
	for _, side := range []string{students.Table, students.EnrollmentsTable} {
		for _, body := range []string{`{"name":"absent"}`, `{"department_code":"absent"}`, `{"status_codes":["absent"]}`, `{"page":1000000}`} {
			repo := studentProfilesRepo()
			repo.tables[side] = append(repo.tables[side], sourceRow(`{"XH":"orphan","XM":"孤立"}`))
			h, _ := studentProfilesHandler(t, repo, read.DefaultLimits())
			w := studentProfilesRequest(h, body, "synthetic-key")
			if w.Code != 500 || !strings.Contains(w.Body.String(), `"data":null`) {
				t.Fatal(side, body, w.Code, w.Body.String())
			}
			if len(repo.calls) != 2 {
				t.Fatal("ancillary read before strict pairing", repo.calls)
			}
			// Exact identity narrows BOTH source tables; unrelated orphans are ignored.
			repo.calls = nil
			w = studentProfilesRequest(h, `{"student_number":"01"}`, "synthetic-key")
			if w.Code != 200 {
				t.Fatal(side, w.Code, w.Body.String())
			}
			for _, call := range repo.calls[:2] {
				if !reflect.DeepEqual(call.filters, []source.Filter{{Column: "XH", Value: "01"}}) {
					t.Fatal(call)
				}
			}
			w = studentProfilesRequest(h, `{"student_number":"orphan"}`, "synthetic-key")
			if w.Code != 500 {
				t.Fatal(w.Code, w.Body.String())
			}
			w = studentProfilesRequest(h, `{"student_number":"missing"}`, "synthetic-key")
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
}

func TestT20_09StudentFailureIntegrityAndBoundedContactReads(t *testing.T) {
	for _, kind := range []string{"auth", "parse", "duplicate", "blank name", "late conversion", "late contact", "SQL", "budget", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			repo := studentProfilesRepo()
			limits := read.DefaultLimits()
			body := `{}`
			key := "synthetic-key"
			status := 500
			switch kind {
			case "auth":
				key = "wrong"
				status = 401
			case "parse":
				body = `{"study_years":1.5}`
				status = 400
			case "duplicate":
				repo.tables[students.EnrollmentsTable] = append(repo.tables[students.EnrollmentsTable], repo.tables[students.EnrollmentsTable][0])
			case "blank name":
				repo.tables[students.Table][0]["XM"] = json.RawMessage(`" "`)
				body = `{"name":"absent"}`
			case "late conversion":
				repo.tables[students.EnrollmentsTable][2]["XZ"] = json.RawMessage(`1.5`)
				body = `{"page_size":1}`
			case "late contact":
				repo.tables[students.ContactsTable][0]["ETL_TIME"] = json.RawMessage(`"bad"`)
			case "SQL":
				repo.fail = students.ContactsTable
				status = 503
			case "budget":
				limits.Rows = 6
				status = 422
			case "cancel":
				status = 504
			}
			h, a := studentProfilesHandler(t, repo, limits)
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
	large := &repository{tables: map[string][]source.SourceRow{}}
	for i := 0; i < 1001; i++ {
		id := fmt.Sprintf("%04d", i)
		large.tables[students.Table] = append(large.tables[students.Table], sourceRow(fmt.Sprintf(`{"XH":%q,"XM":"N"}`, id)))
		large.tables[students.EnrollmentsTable] = append(large.tables[students.EnrollmentsTable], sourceRow(fmt.Sprintf(`{"XH":%q}`, id)))
		large.tables[students.ContactsTable] = append(large.tables[students.ContactsTable], sourceRow(fmt.Sprintf(`{"XH":%q,"YDDH":"same"}`, id)))
	}
	limits := read.DefaultLimits()
	limits.Rows = 2003
	h, _ := studentProfilesHandler(t, large, limits)
	w := studentProfilesRequest(h, `{"page":1001,"page_size":1}`, "synthetic-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":1001`) || !strings.Contains(w.Body.String(), `"student_number":"1000"`) || len(large.calls) != 7 {
		t.Fatal(w.Code, w.Body.String(), len(large.calls))
	}
	limits.Rows = 2002
	h, _ = studentProfilesHandler(t, large, limits)
	w = studentProfilesRequest(h, `{"page_size":1}`, "synthetic-key")
	if w.Code != 422 {
		t.Fatal(w.Code, w.Body.String())
	}
	h, _ = studentProfilesHandler(t, large, read.DefaultLimits())
	w = studentProfilesRequest(h, `{}`, "synthetic-key")
	var response struct{ Data contract.StudentsResponse }
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	if w.Code != 200 || response.Data.Total != 1001 || len(response.Data.Items) != 1000 || response.Data.Items[999].StudentNumber != "0999" {
		t.Fatal(w.Code, response.Data.Total, len(response.Data.Items))
	}
}

// Regression for source RXNY supplied as a full date instead of a month.
// All identity and related records use synthetic fixtures.
func TestT25_07StudentAdmissionFullDate(t *testing.T) {
	for _, admission := range []string{"2025-09-21", "20250921"} {
		t.Run(admission, func(t *testing.T) {
			repo := studentProfilesRepo()
			raw, _ := json.Marshal(admission)
			repo.tables[students.EnrollmentsTable][0]["RXNY"] = raw
			h, _ := studentProfilesHandler(t, repo, read.DefaultLimits())
			for _, body := range []string{
				`{"student_number":"01","page":1,"page_size":20}`,
				`{"student_number":"01","admission_year":"2025"}`,
				`{"admission_year":"2025"}`,
			} {
				w := studentProfilesRequest(h, body, "synthetic-key")
				var response struct{ Data contract.StudentsResponse }
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if w.Code != 200 || response.Data.Total != 1 || len(response.Data.Items) != 1 {
					t.Fatal(w.Code, w.Body.String())
				}
				item := response.Data.Items[0]
				if item.StudentNumber != "01" || item.AdmissionMonth == nil || *item.AdmissionMonth != "2025-09" || item.ExpectedGraduationDate == nil || *item.ExpectedGraduationDate != "2029-06-30" {
					t.Fatal(w.Body.String())
				}
			}
			w := studentProfilesRequest(h, `{"student_number":"01","admission_year":"2024"}`, "synthetic-key")
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) || !strings.Contains(w.Body.String(), `"items":[]`) {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	for _, admission := range []string{"2025-02-29", "20250229", "2025-09-21T00:00:00"} {
		repo := studentProfilesRepo()
		raw, _ := json.Marshal(admission)
		repo.tables[students.EnrollmentsTable][0]["RXNY"] = raw
		h, _ := studentProfilesHandler(t, repo, read.DefaultLimits())
		w := studentProfilesRequest(h, `{"student_number":"01"}`, "synthetic-key")
		if w.Code != 500 || !strings.Contains(w.Body.String(), `"data":null`) {
			t.Fatal(admission, w.Code, w.Body.String())
		}
	}
}
