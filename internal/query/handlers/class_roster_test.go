package handlers_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/students"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/teaching"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var classRosterKey = teaching.Key{AcademicYear: "2026-2027", Semester: "1", Class: "J01", Course: "C1"}

func classRosterRepo() *repository {
	repo := teachingClassesRepo()
	repo.tables[students.Table] = []source.SourceRow{sourceRow(`{"XH":"01","XM":"学生","ETL_TIME":"2026-09-18 09:40:00"}`), sourceRow(`{"XH":"not-member","XM":"同班但无关系","ETL_TIME":"bad"}`)}
	repo.tables[students.EnrollmentsTable] = []source.SourceRow{sourceRow(`{"XH":"01","YXSH":"D1","SZBH":"B1","XSDQZTM":"inactive","XJZTM":"graduated","SFZX":"0","ETL_TIME":"2026-09-18 10:10:00"}`), sourceRow(`{"XH":"001","YXSH":"missing","SZBH":"missing"}`), sourceRow(`{"XH":"not-member","YXSH":"D1","SZBH":"B1"}`)}
	repo.tables[organizations.Table] = []source.SourceRow{sourceRow(`{"DWH":"D1","DWMC":"当前院系","ETL_TIME":"2026-09-18 09:50:00"}`)}
	repo.tables[students.ClassesTable] = []source.SourceRow{sourceRow(`{"BH":"B1","BJMC":"当前班级","ETL_TIME":"2026-09-18 10:00:00"}`)}
	return repo
}

func TestT23_01ActualRosterAndCurrentAffiliations(t *testing.T) {
	repo := classRosterRepo()
	svc, b := teachingService(t, repo)
	rosters, err := (teaching.Membership{Source: repo}).Resolve(t.Context(), pinned, []teaching.Key{classRosterKey}, b)
	if err != nil {
		t.Fatal(err)
	}
	roster := rosters[classRosterKey]
	got, err := svc.StudentRows(t.Context(), pinned, roster, roster.Members, b)
	var want []contract.TeachingClassStudent
	_ = json.Unmarshal([]byte(`[{"student_number":"001","name":null,"department_code":"missing","department_name":null,"class_code":"missing","class_name":null,"source_updated_at":"2026-09-18T08:20:00+08:00"},{"student_number":"01","name":"学生","department_code":"D1","department_name":"当前院系","class_code":"B1","class_name":"当前班级","source_updated_at":"2026-09-18T10:10:00+08:00"}]`), &want)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	for _, call := range repo.calls {
		if call.h != pinned {
			t.Fatal(call)
		}
	}
	if len(repo.calls) != 6 {
		t.Fatal(repo.calls)
	}
}

func TestT23_02MissingProfilesNeverRemoveRealMembers(t *testing.T) {
	for _, kind := range []string{"basic", "enrollment", "both", "department", "class"} {
		repo := classRosterRepo()
		switch kind {
		case "basic":
			repo.tables[students.Table] = nil
		case "enrollment":
			repo.tables[students.EnrollmentsTable] = nil
		case "both":
			repo.tables[students.Table] = nil
			repo.tables[students.EnrollmentsTable] = nil
		case "department":
			repo.tables[organizations.Table] = nil
		case "class":
			repo.tables[students.ClassesTable] = nil
		}
		svc, b := teachingService(t, repo)
		rosters, err := (teaching.Membership{Source: repo}).Resolve(t.Context(), pinned, []teaching.Key{classRosterKey}, b)
		if err != nil {
			t.Fatal(err)
		}
		roster := rosters[classRosterKey]
		got, err := svc.StudentRows(t.Context(), pinned, roster, roster.Members, b)
		if err != nil || len(got) != 2 || got[0].StudentNumber != "001" || got[1].StudentNumber != "01" {
			t.Fatal(kind, got, err)
		}
		switch kind {
		case "basic", "both":
			if got[1].Name != nil {
				t.Fatal(got)
			}
		case "enrollment":
			if got[1].DepartmentCode != nil || got[1].ClassCode != nil {
				t.Fatal(got)
			}
		case "department":
			if got[1].DepartmentName != nil || *got[1].DepartmentCode != "D1" {
				t.Fatal(got)
			}
		case "class":
			if got[1].ClassName != nil || *got[1].ClassCode != "B1" {
				t.Fatal(got)
			}
		}
	}
}

func TestT23_03RosterTimesAndAncillaryFailure(t *testing.T) {
	for _, table := range []string{timetable.SchedulesTable, timetable.RelationsTable, students.Table, students.EnrollmentsTable, organizations.Table, students.ClassesTable} {
		repo := classRosterRepo()
		repo.tables[table][0]["ETL_TIME"] = json.RawMessage(`"2026-09-18 11:00:00"`)
		svc, b := teachingService(t, repo)
		rosters, err := (teaching.Membership{Source: repo}).Resolve(t.Context(), pinned, []teaching.Key{classRosterKey}, b)
		if err != nil {
			t.Fatal(err)
		}
		roster := rosters[classRosterKey]
		got, err := svc.StudentRows(t.Context(), pinned, roster, roster.Members, b)
		if err != nil || len(got) != 2 || *got[1].SourceUpdatedAt != "2026-09-18T11:00:00+08:00" {
			t.Fatal(table, got, err)
		}
	}
	for _, kind := range []string{"duplicate", "bad name", "SQL", "late time"} {
		repo := classRosterRepo()
		svc, b := teachingService(t, repo)
		rosters, err := (teaching.Membership{Source: repo}).Resolve(t.Context(), pinned, []teaching.Key{classRosterKey}, b)
		if err != nil {
			t.Fatal(err)
		}
		roster := rosters[classRosterKey]
		switch kind {
		case "duplicate":
			repo.tables[students.Table] = append(repo.tables[students.Table], repo.tables[students.Table][0])
		case "bad name":
			repo.tables[students.Table][0]["XM"] = json.RawMessage(`123`)
		case "SQL":
			repo.fail = students.ClassesTable
		case "late time":
			repo.tables[students.ClassesTable][0]["ETL_TIME"] = json.RawMessage(`"bad"`)
		}
		got, err := svc.StudentRows(t.Context(), pinned, roster, roster.Members, b)
		if err == nil || got != nil {
			t.Fatal(kind, got, err)
		}
	}
}
