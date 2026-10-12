package handlers_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/staff"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/teaching"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func teachingClassesRepo() *repository {
	return &repository{tables: map[string][]source.SourceRow{
		timetable.SchedulesTable: {sourceRow(`{"PKH":"p1","JXBH":"J01","JHKCH":"C1","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T001","JXBMC":"班","XDRS":"99","JSXM":123,"SKJC":"bad","ETL_TIME":"2026-09-18 08:00:00"}`), sourceRow(`{"PKH":"p2","JXBH":"J01","JHKCH":"C1","KKXND":"2026-2027","KKXQM":"1","JSJGH":"T002,T003","ETL_TIME":"2026-09-18 08:20:00"}`)},
		timetable.RelationsTable: {sourceRow(`{"ID":"r1","JXBH":"J01","JHKCH":"C1","XH":"01","ETL_TIME":"2026-09-18 09:00:00"}`), sourceRow(`{"ID":"r2","JXBH":"J01","JHKCH":"C1","XH":"001"}`), sourceRow(`{"ID":"r3","JXBH":"J01","JHKCH":"C1","XH":"01","ETL_TIME":"2026-09-18 09:30:00"}`)},
		timetable.CoursesTable:   {sourceRow(`{"KCH":"C1","KCMC":"课程","ETL_TIME":"2026-09-18 08:50:00"}`)},
		staff.Table:              {sourceRow(`{"JGH":"T001","XM":"甲","DQZTM":"inactive","ETL_TIME":"2026-09-18 08:30:00"}`), sourceRow(`{"JGH":"T002","XM":"乙","ETL_TIME":"2026-09-18 08:40:00"}`)},
	}}
}
func teacherClassesRequest() contract.StaffTeachingClassesRequest {
	return contract.StaffTeachingClassesRequest{StaffNumber: "T001", AcademicYear: "2026-2027", Semester: "1"}
}
func teachingService(t *testing.T, repo *repository) (teaching.Service, *read.Budget) {
	t.Helper()
	clock := testkit.FakeClock{Time: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}
	times, _ := compute.NewTimes("Asia/Shanghai")
	limits := read.DefaultLimits()
	b, err := read.NewBudget(limits, clock)
	if err != nil {
		t.Fatal(err)
	}
	return teaching.Service{Source: repo, Clock: clock, Times: times, Limits: limits}, b
}

func TestT22_01TeachingClassCourseMappingAndDuplicateRejection(t *testing.T) {
	repo := teachingClassesRepo()
	svc, b := teachingService(t, repo)
	got, err := svc.StaffClasses(t.Context(), pinned, teacherClassesRequest(), b)
	var want []contract.TeachingClass
	_ = json.Unmarshal([]byte(`[{"teaching_class_number":"J01","teaching_class_name":"班","planned_course_code":"C1","course_code":"C1","course_name":"课程","teachers":[{"staff_number":"T001","name":"甲"},{"staff_number":"T002","name":"乙"},{"staff_number":"T003","name":null}],"student_count":2,"source_updated_at":"2026-09-18T09:30:00+08:00"}]`), &want)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	if len(repo.calls) != 5 {
		t.Fatal(repo.calls)
	}
	for _, call := range repo.calls {
		if call.h != pinned {
			t.Fatal(call)
		}
	}
	repo = teachingClassesRepo()
	repo.tables[timetable.CoursesTable] = nil
	svc, b = teachingService(t, repo)
	got, err = svc.StaffClasses(t.Context(), pinned, teacherClassesRequest(), b)
	if err != nil || len(got) != 1 || got[0].CourseCode != "C1" || got[0].PlannedCourseCode != "C1" || got[0].CourseName != nil {
		t.Fatal(got, err)
	}
	for _, table := range []string{timetable.CoursesTable, staff.Table} {
		repo := teachingClassesRepo()
		repo.tables[table] = append(repo.tables[table], repo.tables[table][0])
		svc, b := teachingService(t, repo)
		got, err := svc.StaffClasses(t.Context(), pinned, teacherClassesRequest(), b)
		if err == nil || got != nil {
			t.Fatal(table, got, err)
		}
	}
}

func TestT22_02FullTeacherProfilesAndMissingPrimaryTeacher(t *testing.T) {
	repo := teachingClassesRepo()
	repo.tables[staff.Table] = nil
	svc, b := teachingService(t, repo)
	got, err := svc.StaffClasses(t.Context(), pinned, teacherClassesRequest(), b)
	if err != nil || len(got) != 1 || len(got[0].Teachers) != 3 {
		t.Fatal(got, err)
	}
	for i, id := range []string{"T001", "T002", "T003"} {
		if got[0].Teachers[i].StaffNumber != id || got[0].Teachers[i].Name != nil {
			t.Fatal(got)
		}
	}
	req := teacherClassesRequest()
	req.StaffNumber = "T00"
	svc, b = teachingService(t, repo)
	got, err = svc.StaffClasses(t.Context(), pinned, req, b)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}

func TestT22_03ActualCountAndEveryParticipatingTime(t *testing.T) {
	for _, table := range []string{timetable.SchedulesTable, timetable.CoursesTable, staff.Table, timetable.RelationsTable} {
		repo := teachingClassesRepo()
		repo.tables[table][len(repo.tables[table])-1]["ETL_TIME"] = json.RawMessage(`"2026-09-18 10:00:00"`)
		svc, b := teachingService(t, repo)
		got, err := svc.StaffClasses(t.Context(), pinned, teacherClassesRequest(), b)
		if err != nil || len(got) != 1 || got[0].StudentCount != 2 || got[0].SourceUpdatedAt == nil || *got[0].SourceUpdatedAt != "2026-09-18T10:00:00+08:00" {
			t.Fatal(table, got, err)
		}
	}
	repo := teachingClassesRepo()
	repo.tables[timetable.RelationsTable] = nil
	svc, b := teachingService(t, repo)
	got, err := svc.StaffClasses(t.Context(), pinned, teacherClassesRequest(), b)
	if err != nil || len(got) != 1 || got[0].StudentCount != 0 {
		t.Fatal(got, err)
	}
	repo = teachingClassesRepo()
	repo.tables[timetable.RelationsTable][2]["ETL_TIME"] = json.RawMessage(`"bad"`)
	svc, b = teachingService(t, repo)
	got, err = svc.StaffClasses(t.Context(), pinned, teacherClassesRequest(), b)
	if err == nil || got != nil {
		t.Fatal(got, err)
	}
}
