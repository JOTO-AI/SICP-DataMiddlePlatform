package grades_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func row(raw string) source.SourceRow {
	var r source.SourceRow
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		panic(err)
	}
	return r
}
func base() source.SourceRow {
	return row(`{"ID":"G01","XH":"001","XN":"2025-2026","XQM":"2","KCH":"C01","XF":2,"JD":0,"KCCJ":86,"ETL_TIME":"2026-09-18 08:00:00"}`)
}
func assembler(t *testing.T, data grades.Related) grades.Assembler {
	t.Helper()
	times, err := compute.NewTimes("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	a, err := grades.NewAssembler(data, times)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func fixture() grades.Related {
	return grades.Related{
		Students:    []source.SourceRow{row(`{"XH":"001","XM":"Student","ETL_TIME":"2026-09-18 07:00:00"}`)},
		Enrollments: []source.SourceRow{row(`{"XH":"001","YXSH":"D01","SZBH":"B01","ETL_TIME":"2026-09-18 08:10:00"}`)},
		Departments: []source.SourceRow{row(`{"DWH":"D01","DWMC":"Student Dept","ETL_TIME":"2026-09-18 08:20:00"}`), row(`{"DWH":"D02","DWMC":"Course Dept","ETL_TIME":"2026-09-18 08:50:00"}`), row(`{"DWH":"UNUSED","ETL_TIME":"2026-10-01 00:00:00"}`)},
		Classes:     []source.SourceRow{row(`{"BH":"B01","BJMC":"Class","ETL_TIME":"2026-09-18 08:30:00"}`)},
		Courses:     []source.SourceRow{row(`{"KCH":"C01","KCMC":"Course","XF":3,"ZXS":"48","KCSXM":"unknown-attribute","KCXZM":"unknown-nature","KCLXM":"unknown-type","KKXY":"D02","SFCXKC":"unconfirmed","CXKCH":"R01","CXKCMC":"Retake config","ETL_TIME":"2026-09-18 08:40:00"}`)},
	}
}

func TestT07_06GradeCreditsAndWholeFieldMapping(t *testing.T) {
	got, err := assembler(t, fixture()).Grade(base())
	if err != nil {
		t.Fatal(err)
	}
	var want contract.Grade
	if err := json.Unmarshal([]byte(`{"grade_id":"G01","student":{"student_number":"001","name":"Student","department_code":"D01","department_name":"Student Dept","class_code":"B01","class_name":"Class"},"academic_year":"2025-2026","semester":"2","course":{"course_code":"C01","course_name":"Course","attribute_code":"unknown-attribute","attribute_name":null,"nature_code":"unknown-nature","nature_name":null,"type_code":"unknown-type","type_name":null,"department":{"organization_code":"D02","organization_name":"Course Dept"},"total_hours":48,"retake_flag":"unconfirmed","retake_course_code":"R01","retake_course_name":"Retake config"},"score":86,"grade_point":0,"credits":2,"source_updated_at":"2026-09-18T08:50:00+08:00"}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestT07_08OnlyParticipatingSourceTimesContribute(t *testing.T) {
	got, err := assembler(t, fixture()).Grade(base())
	if err != nil || got.SourceUpdatedAt == nil || *got.SourceUpdatedAt != "2026-09-18T08:50:00+08:00" {
		t.Fatal(got, err)
	}
	r := base()
	delete(r, "ETL_TIME")
	got, err = assembler(t, grades.Related{}).Grade(r)
	if err != nil || got.SourceUpdatedAt != nil {
		t.Fatal(got, err)
	}
}

func TestT07_09MissingAssociationsPreserveRecordsAndCodes(t *testing.T) {
	for _, kind := range []string{"student", "enrollment", "course", "departments", "class", "all"} {
		t.Run(kind, func(t *testing.T) {
			d := fixture()
			switch kind {
			case "student":
				d.Students = nil
			case "enrollment":
				d.Enrollments = nil
			case "course":
				d.Courses = nil
			case "departments":
				d.Departments = nil
			case "class":
				d.Classes = nil
			case "all":
				d = grades.Related{}
			}
			got, err := assembler(t, d).Grade(base())
			if err != nil || got.GradeID != "G01" || got.Student.StudentNumber != "001" || got.Course.CourseCode != "C01" {
				t.Fatal(got, err)
			}
			switch kind {
			case "student":
				if got.Student.Name != nil {
					t.Fatal(got.Student)
				}
			case "enrollment":
				if got.Student.DepartmentCode != nil || got.Student.ClassCode != nil {
					t.Fatal(got.Student)
				}
			case "course":
				if got.Course.CourseName != nil || got.Course.Department != nil {
					t.Fatal(got.Course)
				}
			case "departments":
				if got.Student.DepartmentCode == nil || *got.Student.DepartmentCode != "D01" || got.Student.DepartmentName != nil || got.Course.Department == nil || got.Course.Department.OrganizationCode != "D02" || got.Course.Department.OrganizationName != nil {
					t.Fatal(got)
				}
			case "class":
				if got.Student.ClassCode == nil || *got.Student.ClassCode != "B01" || got.Student.ClassName != nil {
					t.Fatal(got.Student)
				}
			}
		})
	}
}

func TestT07_10UnknownCodesAndRetakeConfigurationStayRaw(t *testing.T) {
	got, err := assembler(t, fixture()).Grade(base())
	if err != nil {
		t.Fatal(err)
	}
	c := got.Course
	if c.AttributeCode == nil || *c.AttributeCode != "unknown-attribute" || c.NatureCode == nil || *c.NatureCode != "unknown-nature" || c.TypeCode == nil || *c.TypeCode != "unknown-type" || c.AttributeName != nil || c.NatureName != nil || c.TypeName != nil || c.RetakeFlag == nil || *c.RetakeFlag != "unconfirmed" || c.RetakeCourseCode == nil || *c.RetakeCourseCode != "R01" {
		t.Fatal(c)
	}
}

func TestT07_11TextScoreIsNullAndNumericZeroIsPreserved(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want *float64
	}{{`"缺考"`, nil}, {`null`, nil}, {`0`, number(0)}, {`"86.5"`, number(86.5)}} {
		r := base()
		r["KCCJ"] = json.RawMessage(tc.raw)
		got, err := assembler(t, grades.Related{}).Grade(r)
		if err != nil || !reflect.DeepEqual(got.Score, tc.want) {
			t.Fatal(tc.raw, got.Score, err)
		}
	}
}
func number(v float64) *float64 { return &v }

func TestT07_09AmbiguousCourseAndMalformedFieldsFailWholeAssembly(t *testing.T) {
	d := fixture()
	d.Courses = append(d.Courses, d.Courses[0])
	times, _ := compute.NewTimes("Asia/Shanghai")
	if _, err := grades.NewAssembler(d, times); err == nil {
		t.Fatal("ambiguous course selected")
	}
	for key, raw := range map[string]string{"ID": `null`, "XH": `1`, "KCCJ": `true`, "XF": `"invalid"`, "ETL_TIME": `"bad"`} {
		r := base()
		r[key] = json.RawMessage(raw)
		got, err := assembler(t, fixture()).Grade(r)
		if err == nil || !reflect.DeepEqual(got, contract.Grade{}) {
			t.Fatal(key, got, err)
		}
	}
}
