package timetable_test

import (
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
)

func TestT10_04CourseAndTeachingClassIntersect(t *testing.T) {
	name := "示例数学"
	other := "示例外语"
	entries := []contract.TimetableEntry{{CourseCode: "C01", CourseName: &name, TeachingClassNumber: "001J"}, {CourseCode: "C01", CourseName: &name, TeachingClassNumber: "002J"}, {CourseCode: "C02", CourseName: &other, TeachingClassNumber: "001J"}}
	q, class := "数学", "001J"
	req := staffRequest()
	req.CourseQuery = &q
	req.TeachingClassNumber = &class
	got := timetable.FilterStaff(entries, req)
	if len(got) != 1 || got[0].CourseCode != "C01" || got[0].TeachingClassNumber != "001J" {
		t.Fatal(got)
	}
	class = "01J"
	if len(timetable.FilterStaff(entries, req)) != 0 {
		t.Fatal("class substring matched")
	}
}

func TestT10_09CourseCodeOrNameMatchesOnlyOnce(t *testing.T) {
	name := "C001 示例数学"
	entries := []contract.TimetableEntry{{CourseCode: "C001", CourseName: &name}}
	for _, tc := range []struct {
		q string
		n int
	}{{"C001", 1}, {"数学", 1}, {"missing", 0}} {
		req := staffRequest()
		req.CourseQuery = &tc.q
		got := timetable.FilterStaff(entries, req)
		if got == nil || len(got) != tc.n {
			t.Fatalf("%s %+v", tc.q, got)
		}
	}
	name = "示例数学"
	q := "C00"
	req := staffRequest()
	req.CourseQuery = &q
	if len(timetable.FilterStaff(entries, req)) != 0 {
		t.Fatal("code substring matched")
	}
	entries[0].CourseName = nil
	q = "C001"
	if len(timetable.FilterStaff(entries, req)) != 1 {
		t.Fatal("missing name lost code match")
	}
}
