package timetable_test

import (
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
)

func TestT09_04TeacherIDsAreWholeDistinctTokens(t *testing.T) {
	got, err := timetable.ParseTeachers(row(`{"JSJGH":" 001,T001,001,T0010 ","JSXM":"甲,乙,甲"}`))
	if err != nil || !reflect.DeepEqual(got.StaffNumbers, []string{"001", "T001", "T0010"}) || !reflect.DeepEqual(got.Names, []string{"甲", "乙"}) {
		t.Fatalf("got %+v %v", got, err)
	}
	for _, id := range []string{"001", "T001", "T0010"} {
		if !got.Contains(id) {
			t.Fatalf("missing %s", id)
		}
	}
	for _, id := range []string{"1", "T00", "T00100", ""} {
		if got.Contains(id) {
			t.Fatalf("substring %s", id)
		}
	}
}

func TestT09_05EmptyAndUnknownTeacherDelimitersAreExplicit(t *testing.T) {
	for _, raw := range []string{`{}`, `{"JSJGH":null,"JSXM":null}`, `{"JSJGH":" ","JSXM":""}`} {
		got, err := timetable.ParseTeachers(row(raw))
		if err != nil || got.StaffNumbers == nil || got.Names == nil || len(got.StaffNumbers)+len(got.Names) != 0 {
			t.Fatalf("empty %+v %v", got, err)
		}
	}
	for _, raw := range []string{`{"JSJGH":"001,,002"}`, `{"JSJGH":",001"}`, `{"JSJGH":"001;002"}`, `{"JSJGH":"001，002"}`, `{"JSJGH":"001/002"}`, `{"JSJGH":"001 002"}`, `{"JSJGH":"001|002"}`, `{"JSJGH":123}`, `{"JSJGH":"001","JSXM":"甲、乙"}`, `{"JSXM":"甲\n乙"}`, `{"JSXM":"甲,"}`} {
		t.Run(raw, func(t *testing.T) {
			got, err := timetable.ParseTeachers(row(raw))
			if err == nil || !reflect.DeepEqual(got, timetable.Teachers{}) {
				t.Fatalf("accepted %+v %v", got, err)
			}
		})
	}
}

func TestT09_06TeacherArraysDoNotRequireSameLength(t *testing.T) {
	got, err := timetable.ParseTeachers(row(`{"JSJGH":"001,002,003","JSXM":"示例教师,Example Teacher"}`))
	if err != nil || !reflect.DeepEqual(got.StaffNumbers, []string{"001", "002", "003"}) || !reflect.DeepEqual(got.Names, []string{"示例教师", "Example Teacher"}) {
		t.Fatalf("got %+v %v", got, err)
	}
}
