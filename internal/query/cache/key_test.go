package cache_test

import (
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func key(t *testing.T, bundle string, op contract.Operation, raw string) string {
	t.Helper()
	req, err := transport.Parse(op, strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	key, err := cache.Key(bundle, op, req)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestT11_01EquivalentDefaultsNullSetsAndNumbersShareKey(t *testing.T) {
	base := key(t, "C1", contract.BatchGrades, `{"academic_year":"all","student_numbers":["002","001","001"],"score_min":0,"course_attribute_codes":["B","A","B"]}`)
	got := key(t, "C1", contract.BatchGrades, `{"page_size":100,"page":1,"score_min":-0.0,"course_name":null,"student_numbers":["001","002"],"academic_year":"all","course_attribute_codes":["A","B"]}`)
	if got != base {
		t.Fatalf("%s != %s", got, base)
	}
	if !strings.Contains(got, "student_numbers=%5B%22001%22%2C%22002%22%5D") || !strings.Contains(got, "page=1&page_size=100&score_min=0") {
		t.Fatal(got)
	}
	implicit, err := cache.Key("C1", contract.StudentGrades, contract.StudentGradesRequest{StudentNumber: "001"})
	if err != nil {
		t.Fatal(err)
	}
	if implicit != key(t, "C1", contract.StudentGrades, `{"student_number":"001","page":null,"page_size":null}`) {
		t.Fatal("typed defaults differ")
	}
}

func TestT11_02EveryQueryIdentityDimensionSeparatesKeys(t *testing.T) {
	seen := map[string]bool{}
	for _, tc := range []struct {
		bundle string
		op     contract.Operation
		body   string
	}{
		{"C1", contract.StudentGrades, `{"student_number":"001"}`},
		{"C2", contract.StudentGrades, `{"student_number":"001"}`},
		{"C1", contract.StudentGrades, `{"student_number":"1"}`},
		{"C1", contract.StudentGrades, `{"student_number":"001","page":2}`},
		{"C1", contract.StudentGrades, `{"student_number":"001","score_max":0}`},
		{"C1", contract.StudentTimetable, `{"student_number":"001","academic_year":"2026-2027","semester":"1"}`},
		{"C1", contract.StaffTimetable, `{"staff_number":"001","academic_year":"2026-2027","semester":"1"}`},
		{"C1", contract.BatchGrades, `{"academic_year":"all","student_numbers":["001"]}`},
	} {
		k := key(t, tc.bundle, tc.op, tc.body)
		if seen[k] {
			t.Fatal("collision", k)
		}
		seen[k] = true
	}
	if _, err := cache.Key("C1", contract.BatchGrades, contract.StudentGradesRequest{}); err == nil {
		t.Fatal("wrong route request type")
	}
	if _, err := cache.Key("", contract.StudentGrades, contract.StudentGradesRequest{}); err == nil {
		t.Fatal("missing combination")
	}
}

func TestT11_03UTF8AndDelimitersUseUnambiguousPercentEncoding(t *testing.T) {
	got := key(t, "C:1", contract.StudentTimetable, `{"student_number":"001","academic_year":"2026-2027","semester":"1","course_query":"中 +&=:%"}`)
	want := "query:C%3A1:students.timetable.query:academic_year=2026-2027&course_query=%E4%B8%AD%20%2B%26%3D%3A%25&semester=1&student_number=001"
	if got != want {
		t.Fatal(got)
	}
	a := key(t, "C1", contract.BatchGrades, `{"academic_year":"all","student_numbers":["a,b","c"]}`)
	b := key(t, "C1", contract.BatchGrades, `{"academic_year":"all","student_numbers":["a","b,c"]}`)
	if a == b {
		t.Fatal("array separator collision")
	}
}
