package students_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/students"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func row(raw string) source.SourceRow {
	var r source.SourceRow
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		panic(err)
	}
	return r
}
func pair() students.Pair {
	return students.Pair{Basic: row(`{"XH":"001","XM":"学生","ETL_TIME":"2026-10-12 08:00:00"}`), Enrollment: row(`{"XH":"001","XSDQZTM":"raw","XJZTM":"enrolled","SFZX":"0","YXSH":"D1","SSXQH":"X1","ZYH":"Z1","SZNJ":"2026","SZBH":"B1","ETL_TIME":"2026-10-12 08:10:00"}`)}
}
func related() students.Related {
	return students.Related{
		Departments: []source.SourceRow{row(`{"DWH":"D1","DWMC":"院","DWYXBSM":"1","SFSC":"0"}`)},
		Campuses:    []source.SourceRow{row(`{"XQH":"X1","XQMC":"校区"}`)},
		Majors:      []source.SourceRow{row(`{"ZYH":"Z1","ZYMC":"专业"}`)},
		Classes:     []source.SourceRow{row(`{"BH":"B1","BJMC":"班","FDYJGH":"T1"}`)},
		Counselors:  []source.SourceRow{row(`{"JGH":"T1","XM":"教师","ETL_TIME":"2026-10-12 09:00:00"}`), row(`{"JGH":"unrelated","ETL_TIME":"bad"}`)},
		Contacts:    []source.SourceRow{row(`{"XH":"001","DZXX":" raw invalid ","YDDH":"shared","ETL_TIME":"2026-10-12 08:30:00"}`)},
	}
}
func assembler(t *testing.T, data students.Related) students.Assembler {
	t.Helper()
	times, e := compute.NewTimes("Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	a, e := students.NewAssembler(data, times)
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func TestT20_01StrictBidirectionalIdentity(t *testing.T) {
	p := pair()
	got, err := students.PairRecords([]source.SourceRow{p.Basic}, []source.SourceRow{p.Enrollment}, nil)
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], p) {
		t.Fatal(got, err)
	}
	for _, tc := range []struct {
		name                string
		basics, enrollments []source.SourceRow
	}{
		{"no enrollment", []source.SourceRow{p.Basic}, nil},
		{"no basic", nil, []source.SourceRow{p.Enrollment}},
		{"equal count different identity", []source.SourceRow{p.Basic}, []source.SourceRow{row(`{"XH":"002"}`)}},
		{"duplicate basic", []source.SourceRow{p.Basic, p.Basic}, []source.SourceRow{p.Enrollment}},
		{"duplicate enrollment", []source.SourceRow{p.Basic}, []source.SourceRow{p.Enrollment, p.Enrollment}},
		{"blank name", []source.SourceRow{row(`{"XH":"001","XM":" "}`)}, []source.SourceRow{p.Enrollment}},
		{"bad identity", []source.SourceRow{row(`{"XH":123,"XM":"N"}`)}, []source.SourceRow{p.Enrollment}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := students.PairRecords(tc.basics, tc.enrollments, nil)
			if err == nil || got != nil {
				t.Fatal(got, err)
			}
		})
	}
	if got, err := students.PairRecords(nil, nil, nil); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	stop := errors.New("cancelled")
	if got, err := students.PairRecords([]source.SourceRow{p.Basic}, []source.SourceRow{p.Enrollment}, func() error { return stop }); !errors.Is(err, stop) || got != nil {
		t.Fatal(got, err)
	}
	p.Enrollment["XH"] = json.RawMessage(`"002"`)
	if got, err := assembler(t, related()).Member(p); err == nil || !reflect.DeepEqual(got, contract.Student{}) {
		t.Fatal(got, err)
	}
}

func TestT20_02StudentAssociationsAndParticipatingTimes(t *testing.T) {
	got, err := assembler(t, related()).Member(pair())
	var want contract.Student
	_ = json.Unmarshal([]byte(`{"student_number":"001","name":"学生","status_code":"raw","enrollment_status_code":"enrolled","on_campus_flag":"0","department":{"organization_code":"D1","organization_name":"院","parent_organization_code":null,"category_code":null,"category_name":null,"is_active":true},"campus_code":"X1","campus_name":"校区","major_code":"Z1","major_name":"专业","grade":"2026","class_code":"B1","class_name":"班","student_category_code":null,"student_category_name":null,"study_years":null,"admission_month":null,"expected_graduation_date":null,"counselor":{"staff_number":"T1","name":"教师"},"email":" raw invalid ","mobile":"shared","source_updated_at":"2026-10-12T09:00:00+08:00"}`), &want)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	// Every actual associated table contributes; unrelated rows do not.
	for _, kind := range []string{"basic", "enrollment", "department", "campus", "major", "class", "counselor", "contact"} {
		p, d := pair(), related()
		rows := map[string]source.SourceRow{"basic": p.Basic, "enrollment": p.Enrollment, "department": d.Departments[0], "campus": d.Campuses[0], "major": d.Majors[0], "class": d.Classes[0], "counselor": d.Counselors[0], "contact": d.Contacts[0]}
		rows[kind]["ETL_TIME"] = json.RawMessage(`"2026-10-12 10:00:00"`)
		got, err := assembler(t, d).Member(p)
		if err != nil || got.SourceUpdatedAt == nil || *got.SourceUpdatedAt != "2026-10-12T10:00:00+08:00" {
			t.Fatal(kind, got, err)
		}
	}
}

func TestT20_03StudentMissingAncillaryAndInvalidFields(t *testing.T) {
	got, err := assembler(t, students.Related{}).Member(pair())
	if err != nil || got.Department == nil || got.Department.OrganizationCode != "D1" || got.Department.IsActive != nil || *got.CampusCode != "X1" || got.CampusName != nil || *got.MajorCode != "Z1" || got.MajorName != nil || *got.ClassCode != "B1" || got.ClassName != nil || got.Counselor != nil || got.Email != nil {
		t.Fatal(got, err)
	}
	d := related()
	d.Counselors = nil
	got, err = assembler(t, d).Member(pair())
	if err != nil || got.Counselor == nil || got.Counselor.StaffNumber != "T1" || got.Counselor.Name != nil {
		t.Fatal(got, err)
	}
	delete(d.Classes[0], "FDYJGH")
	got, err = assembler(t, d).Member(pair())
	if err != nil || got.Counselor != nil {
		t.Fatal(got, err)
	}
	p := pair()
	p.Enrollment = row(`{"XH":"001"}`)
	got, err = assembler(t, related()).Member(p)
	if err != nil || got.Department != nil || got.CampusCode != nil || got.MajorCode != nil || got.ClassCode != nil || got.Counselor != nil {
		t.Fatal(got, err)
	}
	times, _ := compute.NewTimes("Asia/Shanghai")
	for _, kind := range []string{"department", "campus", "major", "class", "counselor", "contact"} {
		d := related()
		groups := map[string]*[]source.SourceRow{"department": &d.Departments, "campus": &d.Campuses, "major": &d.Majors, "class": &d.Classes, "counselor": &d.Counselors, "contact": &d.Contacts}
		group := groups[kind]
		*group = append(*group, (*group)[0])
		if _, err := students.NewAssembler(d, times); err == nil {
			t.Fatal(kind, "duplicate accepted")
		}
	}
	d = related()
	d.Counselors[0]["XM"] = json.RawMessage(`123`)
	got, err = assembler(t, d).Member(pair())
	if err == nil || !reflect.DeepEqual(got, contract.Student{}) {
		t.Fatal(got, err)
	}
}
