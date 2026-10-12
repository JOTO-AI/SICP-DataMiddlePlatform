package teaching_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/teaching"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func row(raw string) source.SourceRow {
	var r source.SourceRow
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		panic(err)
	}
	return r
}
func schedule(id, class, course, teachers string) source.SourceRow {
	r := row(`{"KKXND":"2026-2027","KKXQM":"1","SKRQ":"bad","SKJC":123,"XDRS":"bad","JSXM":123}`)
	for field, value := range map[string]string{"PKH": id, "JXBH": class, "JHKCH": course, "JSJGH": teachers} {
		r[field], _ = json.Marshal(value)
	}
	return r
}
func request() contract.StaffTeachingClassesRequest {
	return contract.StaffTeachingClassesRequest{StaffNumber: "T001", AcademicYear: "2026-2027", Semester: "1"}
}

func TestT21_01WholeTeacherTokensAndSharedParser(t *testing.T) {
	r := schedule("p1", "001", "C1", " T001,T0010,T001,001 ")
	ids, err := timetable.ParseTeacherIDs(r)
	if err != nil || !reflect.DeepEqual(ids, []string{"T001", "T0010", "001"}) {
		t.Fatal(ids, err)
	}
	// Existing timetable parser still requires valid display names.
	if _, err := timetable.ParseTeachers(r); err == nil {
		t.Fatal("old display-name validation changed")
	}
	for _, id := range []string{"T001", "T0010", "001", "T00", "1"} {
		req := request()
		req.StaffNumber = id
		got, err := teaching.ForTeacher([]source.SourceRow{r}, req, nil)
		want := 0
		if id == "T001" || id == "T0010" || id == "001" {
			want = 1
		}
		if err != nil || len(got) != want {
			t.Fatal(id, got, err)
		}
	}
	for _, bad := range []string{"T001;T002", "T001，T002", "T001/T002", "T001 T002", "T001,,T002"} {
		r := schedule("p", "J", "C", bad)
		if _, err := teaching.ForTeacher([]source.SourceRow{r}, request(), nil); err == nil {
			t.Fatal(bad)
		}
	}
}

func TestT21_02GroupAllLessonsAndFullCombination(t *testing.T) {
	rows := []source.SourceRow{schedule("p1", "01", "C1", "T001"), schedule("p2", "01", "C1", "T002,T002"), schedule("p3", "01", "C2", "T001"), schedule("p4", "001", "C1", "T0010")}
	rows[1]["JXBMC"] = json.RawMessage(`"班名"`)
	got, err := teaching.ForTeacher(rows, request(), nil)
	if err != nil || len(got) != 2 || got[0].Key.Class != "01" || got[0].Key.Course != "C1" || *got[0].Name != "班名" || !reflect.DeepEqual(got[0].TeacherNumbers, []string{"T001", "T002"}) || len(got[0].Schedules) != 2 || got[1].Key.Course != "C2" {
		t.Fatal(got, err)
	}
	req := request()
	course := "C2"
	req.CourseCode = &course
	got, err = teaching.ForTeacher(rows, req, nil)
	if err != nil || len(got) != 1 || got[0].Key.Course != "C2" {
		t.Fatal(got, err)
	}
	rows[1]["KKXQM"] = json.RawMessage(`"2"`)
	got, err = teaching.Aggregate(rows, nil)
	if err != nil || len(got) != 4 {
		t.Fatal(got, err)
	}
}

func TestT21_03NamesAndIncompleteOrInvalidIdentity(t *testing.T) {
	malformed := schedule("p", "J", "C", "T001")
	malformed["KKXND"] = json.RawMessage(`"2026-2028"`)
	malformed["KKXQM"] = json.RawMessage(`null`)
	if got, err := teaching.Aggregate([]source.SourceRow{malformed}, nil); err == nil || got != nil {
		t.Fatal("missing semester hid malformed year", got, err)
	}
	for _, field := range []string{"JXBH", "JHKCH", "KKXND", "KKXQM"} {
		for _, empty := range []string{`null`, `""`, `" "`} {
			r := schedule("p", "J", "C", "T001")
			r[field] = json.RawMessage(empty)
			got, err := teaching.Aggregate([]source.SourceRow{r}, nil)
			if err != nil || len(got) != 0 {
				t.Fatal(field, empty, got, err)
			}
		}
	}
	for _, tc := range []struct{ field, raw string }{{"JXBH", `123`}, {"JHKCH", `"C\n"`}, {"KKXND", `"2026-2028"`}, {"KKXQM", `"3"`}, {"JXBMC", `123`}} {
		r := schedule("p", "J", "C", "T001")
		r[tc.field] = json.RawMessage(tc.raw)
		if got, err := teaching.Aggregate([]source.SourceRow{r}, nil); err == nil || got != nil {
			t.Fatal(tc, got, err)
		}
	}
	rows := []source.SourceRow{schedule("p1", "J", "C", "T001"), schedule("p2", "J", "C", "T002")}
	rows[0]["JXBMC"] = json.RawMessage(`"one"`)
	rows[1]["JXBMC"] = json.RawMessage(`"two"`)
	if got, err := teaching.ForTeacher(rows, request(), nil); err == nil || got != nil {
		t.Fatal(got, err)
	}
	rows[1] = rows[0]
	if got, err := teaching.Aggregate(rows, nil); err == nil || got != nil {
		t.Fatal(got, err)
	}
	stop := errors.New("cancelled")
	if got, err := teaching.ForTeacher(rows, request(), func() error { return stop }); !errors.Is(err, stop) || got != nil {
		t.Fatal(got, err)
	}
}
