package organizations_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func row(raw string) source.SourceRow {
	var r source.SourceRow
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		panic(err)
	}
	return r
}
func times(t *testing.T) compute.Times {
	t.Helper()
	v, err := compute.NewTimes("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func root() source.SourceRow {
	return row(`{"DWH":"10001","DWMC":"学校","LSDWH":123,"DWLBM":"5","DWYXBSM":"1","SFSC":"0","ETL_TIME":"2026-10-12 08:00:00"}`)
}

func TestT18_01RootAndWholeGraphValidation(t *testing.T) {
	valid := []source.SourceRow{root(), row(`{"DWH":"001","DWMC":"院系","LSDWH":"10001","DWLBM":"1"}`)}
	got, err := organizations.Assemble(valid, times(t), nil)
	if err != nil || len(got) != 2 || got[0].ParentOrganizationCode != nil || !got[0].IsActive || *got[0].CategoryName != "行政机构" || *got[0].SourceUpdatedAt != "2026-10-12T08:00:00+08:00" || *got[1].CategoryName != "教学院系" {
		t.Fatal(got, err)
	}
	for name, rows := range map[string][]source.SourceRow{
		"empty": nil, "no root": valid[1:], "duplicate": {root(), root()},
		"blank name": {root(), row(`{"DWH":"001","DWMC":" ","LSDWH":"10001"}`)},
		"blank code": {root(), row(`{"DWH":" ","DWMC":"N"}`)},
		"no parent":  {root(), row(`{"DWH":"001","DWMC":"N"}`)},
		"orphan":     {root(), row(`{"DWH":"001","DWMC":"N","LSDWH":"404"}`)},
		"self cycle": {root(), row(`{"DWH":"001","DWMC":"N","LSDWH":"001"}`)},
		"cycle":      {root(), row(`{"DWH":"001","DWMC":"N","LSDWH":"002"}`), row(`{"DWH":"002","DWMC":"N","LSDWH":"001"}`)},
		"bad time":   {row(`{"DWH":"10001","DWMC":"N","ETL_TIME":"bad"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := organizations.Assemble(rows, times(t), nil)
			if err == nil || got != nil {
				t.Fatal(got, err)
			}
		})
	}
	// A deep tree exercises iterative traversal and shared cancellation checks.
	deep := []source.SourceRow{root()}
	for i := 0; i < 10000; i++ {
		parent := "10001"
		if i > 0 {
			parent = fmt.Sprintf("D%d", i-1)
		}
		deep = append(deep, row(fmt.Sprintf(`{"DWH":"D%d","DWMC":"N","LSDWH":%q}`, i, parent)))
	}
	if got, err := organizations.Assemble(deep, times(t), nil); err != nil || len(got) != 10001 {
		t.Fatal(len(got), err)
	}
	stop := errors.New("budget exhausted")
	calls := 0
	got, err = organizations.Assemble(valid, times(t), func() error {
		calls++
		if calls > len(valid) {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || got != nil {
		t.Fatal(got, err)
	}
}

func TestT18_02StatusAndCategoryBoundaries(t *testing.T) {
	for _, active := range []string{`"1"`, `"0"`, `"unknown"`, `""`, `null`} {
		for _, deleted := range []string{`"0"`, `"1"`, `"unknown"`, `""`, `null`} {
			r := row(fmt.Sprintf(`{"DWH":"X","DWMC":"N","DWYXBSM":%s,"SFSC":%s,"DWLBM":"unknown"}`, active, deleted))
			got, err := organizations.Department("X", r)
			if err != nil || got.IsActive == nil || *got.IsActive != (active == `"1"` && deleted == `"0"`) || got.CategoryName != nil || *got.CategoryCode != "unknown" {
				t.Fatal(got, err)
			}
		}
	}
	for _, field := range []string{"DWMC", "LSDWH", "DWLBM", "DWYXBSM", "SFSC"} {
		r := row(`{"DWH":"X","DWMC":"N"}`)
		r[field] = json.RawMessage(`123`)
		if got, err := organizations.Department("X", r); err == nil || got != nil {
			t.Fatal(field, got, err)
		}
	}
}

func TestT18_03DepartmentNullAndLocalValidation(t *testing.T) {
	got, err := organizations.Department("001", nil)
	if err != nil || !reflect.DeepEqual(got, &contract.Department{OrganizationCode: "001"}) {
		t.Fatal(got, err)
	}
	for _, code := range []string{"", "  "} {
		if got, err := organizations.Department(code, nil); err != nil || got != nil {
			t.Fatal(got, err)
		}
	}
	got, err = organizations.Department("X", row(`{"DWH":"X","DWMC":"N","LSDWH":"missing","DWLBM":" "}`))
	if err != nil || got.IsActive == nil || *got.IsActive || got.CategoryCode != nil || got.CategoryName != nil || *got.ParentOrganizationCode != "missing" {
		t.Fatal(got, err)
	}
	if got, err := organizations.Department("different", root()); err == nil || got != nil {
		t.Fatal(got, err)
	}
	got, err = organizations.Department("10001", root())
	if err != nil || got.ParentOrganizationCode != nil {
		t.Fatal(got, err)
	}
}
