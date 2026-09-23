package timetable_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func scheduleRow() source.SourceRow {
	r := calendarRow()
	for k, v := range row(`{"PKH":"P001","JHKCH":"000C","JXBH":"000J","JXBMC":"教学班","SKJSH":"00101","JXDD":"示例教室","JSJGH":"001,T001","JSXM":"示例教师","ETL_TIME":"2026-09-18 07:00:00"}`) {
		r[k] = v
	}
	return r
}
func assembler(t *testing.T, courses ...source.SourceRow) timetable.Assembler {
	t.Helper()
	times, err := compute.NewTimes("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	a, err := timetable.NewAssembler(courses, times)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestT09_07MissingCourseCountAndLeadingZerosRemain(t *testing.T) {
	a := assembler(t, row(`{"KCH":"C","KCMC":"不应按截断编号关联","XF":99}`))
	got, err := a.Schedule(scheduleRow())
	if err != nil {
		t.Fatal(err)
	}
	want := row(`{"course_code":"000C","course_name":null,"planned_course_code":"000C","teaching_class_number":"000J","teaching_class_name":"教学班","enrolled_count":null,"teacher_staff_numbers":["001","T001"],"room_code":"00101","credits":null,"teacher_names":["示例教师"],"location":"示例教室","weekday":1,"sections":[1,2],"weeks":[1,3],"dates":["2026-09-07","2026-09-21"],"source_updated_at":"2026-09-18T07:00:00+08:00"}`)
	data, err := json.Marshal(got.Entry)
	if err != nil {
		t.Fatal(err)
	}
	if got.PKH != "P001" || !reflect.DeepEqual(row(string(data)), want) {
		t.Fatalf("entry %s", data)
	}
	a = assembler(t, row(`{"KCH":"000C","KCMC":"当前课程","XF":3,"ETL_TIME":"2026-09-18 08:00:00"}`), row(`{"KCH":"OTHER","ETL_TIME":"2026-09-19 08:00:00"}`))
	r := scheduleRow()
	r["XDRS"] = json.RawMessage(`0`)
	got, err = a.Schedule(r, row(`{"ETL_TIME":"2026-09-18 09:00:00"}`))
	if err != nil || got.Entry.EnrolledCount == nil || *got.Entry.EnrolledCount != 0 || got.Entry.Credits == nil || *got.Entry.Credits != 3 || got.Entry.CourseName == nil || *got.Entry.CourseName != "当前课程" || got.Entry.SourceUpdatedAt == nil || *got.Entry.SourceUpdatedAt != "2026-09-18T09:00:00+08:00" {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestT09_08SparseRequiredFieldsAndConflictsFail(t *testing.T) {
	a := assembler(t)
	for _, field := range []string{"PKH", "JHKCH", "JXBH", "SKRQ", "XQJ", "SKJC", "ZC"} {
		t.Run(field, func(t *testing.T) {
			r := scheduleRow()
			delete(r, field)
			got, err := a.Schedule(r)
			if err == nil || !reflect.DeepEqual(got, compute.Schedule{}) {
				t.Fatalf("partial %+v %v", got, err)
			}
		})
	}
	for _, raw := range []string{`-1`, `1.5`, `"text"`, `9007199254740992`} {
		r := scheduleRow()
		r["XDRS"] = json.RawMessage(raw)
		if _, err := a.Schedule(r); err == nil {
			t.Fatalf("accepted count %s", raw)
		}
	}
	times, _ := compute.NewTimes("Asia/Shanghai")
	if _, err := timetable.NewAssembler([]source.SourceRow{row(`{"KCH":"000C"}`), row(`{"KCH":"000C"}`)}, times); err == nil {
		t.Fatal("duplicate course accepted")
	}
	got, err := a.Schedule(scheduleRow(), row(`{"ETL_TIME":"invalid"}`))
	if err == nil || !reflect.DeepEqual(got, compute.Schedule{}) {
		t.Fatalf("late partial %+v %v", got, err)
	}
	r := scheduleRow()
	for _, key := range []string{"JSJGH", "JSXM", "JXBMC", "SKJSH", "JXDD", "ETL_TIME"} {
		delete(r, key)
	}
	got, err = a.Schedule(r)
	if err != nil || len(got.Entry.TeacherNames) != 0 || got.Entry.Location != nil || got.Entry.SourceUpdatedAt != nil {
		t.Fatalf("optional %+v %v", got, err)
	}
}
