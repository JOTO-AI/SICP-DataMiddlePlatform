package timetable_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestT10_02RepeatedTeacherTokenKeepsOneArrangementAndAllTeachers(t *testing.T) {
	s := termSchedule()
	s["JSJGH"] = json.RawMessage(`"T001,T002,T001"`)
	got, err := assembler(t).Staff([]source.SourceRow{s}, staffRequest())
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].TeacherStaffNumbers, contract.Array[string]{"T001", "T002"}) {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestT10_03DifferentClassesDatesAndRoomsRemainSeparate(t *testing.T) {
	s := termSchedule()
	s["PKH"] = json.RawMessage(`"P002"`)
	s["JXBH"] = json.RawMessage(`"OTHER"`)
	s["SKJSH"] = json.RawMessage(`"00999"`)
	s["SKRQ"] = json.RawMessage(`"20260928"`)
	got, err := assembler(t).Staff([]source.SourceRow{s, termSchedule()}, staffRequest())
	if err != nil || len(got) != 2 || got[0].TeachingClassNumber != "000J" || got[1].TeachingClassNumber != "OTHER" || got[1].RoomCode == nil || *got[1].RoomCode != "00999" || !reflect.DeepEqual(got[1].Dates, contract.Array[string]{"2026-09-28"}) {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestT10_07StudentAndStaffShareAllTimetableFields(t *testing.T) {
	a := assembler(t, row(`{"KCH":"000C","KCMC":"示例课程","XF":3}`))
	student, err := a.Student([]source.SourceRow{membership()}, []source.SourceRow{termSchedule()}, studentRequest())
	if err != nil {
		t.Fatal(err)
	}
	staff, err := a.Staff([]source.SourceRow{termSchedule()}, staffRequest())
	if err != nil || len(staff) != 1 || !reflect.DeepEqual(student, staff) {
		t.Fatalf("student %+v staff %+v %v", student, staff, err)
	}
}
