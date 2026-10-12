package staff_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/staff"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func row(raw string) source.SourceRow {
	var r source.SourceRow
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		panic(err)
	}
	return r
}
func assembler(t *testing.T, data staff.Related) staff.Assembler {
	t.Helper()
	times, err := compute.NewTimes("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	a, err := staff.NewAssembler(data, times)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func base() source.SourceRow {
	return row(`{"JGH":"001","XM":"甲","DWH":"D1","DQZTM":"unconfirmed","ETL_TIME":"2026-10-12 08:00:00"}`)
}
func related() staff.Related {
	return staff.Related{
		Contacts:    []source.SourceRow{row(`{"JGH":"001","DZXX":" invalid email ","YDDH":"+86 invalid phone","ETL_TIME":"2026-10-12 08:30:00"}`), row(`{"JGH":"unused","ETL_TIME":"invalid"}`)},
		Departments: []source.SourceRow{row(`{"DWH":"D1","DWMC":"院系","LSDWH":"missing","DWLBM":"1","DWYXBSM":"1","SFSC":"0","ETL_TIME":"2026-10-12 08:20:00"}`)},
	}
}

func TestT19_01RequiredIdentityAndCompleteMapping(t *testing.T) {
	got, err := assembler(t, related()).Member(base())
	var want contract.StaffMember
	if e := json.Unmarshal([]byte(`{"staff_number":"001","name":"甲","status_code":"unconfirmed","department":{"organization_code":"D1","organization_name":"院系","parent_organization_code":"missing","category_code":"1","category_name":"教学院系","is_active":true},"email":" invalid email ","mobile":"+86 invalid phone","source_updated_at":"2026-10-12T08:30:00+08:00"}`), &want); e != nil {
		t.Fatal(e)
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	for _, field := range []string{"JGH", "XM"} {
		for _, value := range []string{`null`, `""`, `"  "`, `123`} {
			r := base()
			r[field] = json.RawMessage(value)
			got, err := assembler(t, related()).Member(r)
			if err == nil || !reflect.DeepEqual(got, contract.StaffMember{}) {
				t.Fatal(field, value, got, err)
			}
		}
	}
	r := base()
	r["DQZTM"] = json.RawMessage(`123`)
	if _, err := assembler(t, related()).Member(r); err == nil {
		t.Fatal("numeric status accepted")
	}
}

func TestT19_02MissingAssociationsAndNoContactCleansing(t *testing.T) {
	got, err := assembler(t, staff.Related{}).Member(base())
	if err != nil || got.Email != nil || got.Mobile != nil || !reflect.DeepEqual(got.Department, &contract.Department{OrganizationCode: "D1"}) || *got.SourceUpdatedAt != "2026-10-12T08:00:00+08:00" {
		t.Fatal(got, err)
	}
	r := base()
	delete(r, "DWH")
	delete(r, "ETL_TIME")
	delete(r, "DQZTM")
	got, err = assembler(t, staff.Related{}).Member(r)
	if err != nil || got.Department != nil || got.SourceUpdatedAt != nil || got.StatusCode != nil {
		t.Fatal(got, err)
	}
	d := related()
	d.Contacts = append(d.Contacts, row(`{"JGH":"002","DZXX":" invalid email ","YDDH":"+86 invalid phone"}`))
	a := assembler(t, d)
	r = base()
	r["JGH"] = json.RawMessage(`"002"`)
	second, err := a.Member(r)
	first, e := a.Member(base())
	if err != nil || e != nil || *second.Mobile != *first.Mobile || *second.Email != *first.Email || second.StaffNumber == first.StaffNumber {
		t.Fatal(first, second, err, e)
	}
}

func TestT19_03WholeRowWinnersAndLateErrors(t *testing.T) {
	d := related()
	d.Contacts = []source.SourceRow{row(`{"JGH":"001","DZXX":null,"YDDH":"","ETL_TIME":"2026-10-12 09:00:00"}`)}
	got, err := assembler(t, d).Member(base())
	if err != nil || got.Email != nil || got.Mobile != nil || *got.SourceUpdatedAt != "2026-10-12T09:00:00+08:00" {
		t.Fatal(got, err)
	}
	times, _ := compute.NewTimes("Asia/Shanghai")
	for _, kind := range []string{"contact", "department"} {
		d := related()
		if kind == "contact" {
			d.Contacts = append(d.Contacts, d.Contacts[0])
		} else {
			d.Departments = append(d.Departments, d.Departments[0])
		}
		if _, err := staff.NewAssembler(d, times); err == nil {
			t.Fatal("duplicate accepted", kind)
		}
	}
	for _, field := range []string{"DZXX", "YDDH", "ETL_TIME"} {
		d := related()
		d.Contacts[0][field] = json.RawMessage(`123`)
		got, err := assembler(t, d).Member(base())
		if err == nil || !reflect.DeepEqual(got, contract.StaffMember{}) {
			t.Fatal(field, got, err)
		}
	}
}
