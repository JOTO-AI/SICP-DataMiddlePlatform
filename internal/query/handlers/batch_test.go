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

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func batchRepo() *repository {
	rows := []source.SourceRow{}
	for i := 5; i >= 1; i-- {
		student := "001"
		if i%2 == 0 {
			student = "002"
		}
		rows = append(rows, sourceRow(fmt.Sprintf(`{"ID":"G%02d","XH":"%s","XN":"2025-2026","XQM":"1","KCH":"C01","KCCJ":80}`, i, student)))
	}
	return &repository{tables: map[string][]source.SourceRow{grades.GradesTable: rows}}
}
func batchRequest(h http.Handler, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/sicp/students/grades/batch-query", strings.NewReader(body))
	r.Header.Set("X-API-Key", "synthetic-key")
	h.ServeHTTP(w, r)
	return w
}
func TestT08_07TwoStudentsFiveGradesAcrossThreePages(t *testing.T) {
	r := batchRepo()
	h, _, log, _ := setupOperation(t, r, "", contract.BatchGrades, read.DefaultLimits())
	ids := []string{}
	for page := 1; page <= 3; page++ {
		w := batchRequest(h, fmt.Sprintf(`{"academic_year":"2025-2026","page":%d,"page_size":2}`, page))
		var envelope struct {
			Data contract.GradesResponse `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || envelope.Data.Total != 5 {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, g := range envelope.Data.Items {
			ids = append(ids, g.GradeID)
		}
	}
	if !reflect.DeepEqual(ids, []string{"G01", "G03", "G05", "G02", "G04"}) {
		t.Fatal(ids)
	}
	for _, event := range *log {
		if event.Metadata.Operation != contract.BatchGrades {
			t.Fatal("wrong operation")
		}
	}
}

func TestT08_11HTTPPreservesSetAndAffiliationWithoutExtraRequirements(t *testing.T) {
	r := batchRepo()
	r.tables[grades.EnrollmentsTable] = []source.SourceRow{sourceRow(`{"XH":"001","YXSH":"A","SZBH":"B"}`), sourceRow(`{"XH":"002","YXSH":"Other","SZBH":"B"}`)}
	h, a, _, _ := setupOperation(t, r, "", contract.BatchGrades, read.DefaultLimits())
	w := batchRequest(h, `{"academic_year":"all","student_numbers":["001","001","002"],"student_department_code":"A","class_code":"B"}`)
	var envelope struct {
		Data contract.GradesResponse `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	if w.Code != 200 || envelope.Data.Total != 3 || len(envelope.Data.Items) != 3 || a.calls != 1 || a.closed != 1 {
		t.Fatal(w.Code, w.Body.String(), a)
	}
	gradeCalls := 0
	for _, call := range r.calls {
		if call.h != pinned {
			t.Fatal(call)
		}
		if call.table == grades.GradesTable {
			gradeCalls++
		}
	}
	if gradeCalls != 2 {
		t.Fatal("duplicate student re-read", gradeCalls)
	}
	r = &repository{}
	h, _, _, _ = setupOperation(t, r, "", contract.BatchGrades, read.DefaultLimits())
	w = batchRequest(h, `{"academic_year":"2025-2026"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = batchRequest(h, `{}`)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestT08_10CanceledAndOverBudgetReadsFailWholeHTTPResponse(t *testing.T) {
	for _, kind := range []string{"budget", "cancel", "dependency"} {
		t.Run(kind, func(t *testing.T) {
			r := batchRepo()
			limits := read.DefaultLimits()
			want := 422
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "budget":
				limits.Rows = 4
			case "cancel":
				r.cancel = cancel
				want = 504
			case "dependency":
				r.fail = grades.CoursesTable
				want = 503
			}
			h, a, _, _ := setupOperation(t, r, "", contract.BatchGrades, limits)
			req := httptest.NewRequest("POST", "/api/v1/sicp/students/grades/batch-query", strings.NewReader(`{"academic_year":"all"}`)).WithContext(ctx)
			req.Header.Set("X-API-Key", "synthetic-key")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != want || !strings.Contains(w.Body.String(), `"data":null`) || strings.Contains(w.Body.String(), "G01") || a.closed != 1 {
				t.Fatal(w.Code, w.Body.String(), a)
			}
		})
	}
}
