package students

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const (
	Table            = "dwd_bzksjbsjzlb"
	EnrollmentsTable = "dwd_bzksxjjbsjzlb"
	ContactsTable    = "dwd_bzksgrtxsjzlb"
	CampusesTable    = "dwd_xqjbsjzlb"
	MajorsTable      = "dwd_bzkszyxxsjzlb"
	ClassesTable     = "dwd_bzksbjsjzlb"
)

type Pair struct{ Basic, Enrollment source.SourceRow }

// PairRecords validates both directions before any business filter. Callers
// reading one exact identity must restrict both source scans to that identity.
func PairRecords(basics, enrollments []source.SourceRow, check func() error) ([]Pair, error) {
	indexRows := func(rows []source.SourceRow) (compute.Index, error) {
		if check != nil {
			if err := check(); err != nil {
				return nil, err
			}
		}
		index := compute.Index{}
		for _, r := range rows {
			if check != nil {
				if err := check(); err != nil {
					return nil, err
				}
			}
			id, err := compute.Required(r, "XH")
			if err != nil {
				return nil, err
			}
			if _, exists := index[id]; exists {
				return nil, compute.ErrRecords
			}
			index[id] = r
		}
		return index, nil
	}
	basicIndex, err := indexRows(basics)
	if err != nil {
		return nil, err
	}
	enrollmentIndex, err := indexRows(enrollments)
	if err != nil {
		return nil, err
	}
	if len(basicIndex) != len(enrollmentIndex) {
		return nil, compute.ErrRecords
	}
	result := make([]Pair, 0, len(basics))
	for _, basic := range basics {
		if check != nil {
			if err := check(); err != nil {
				return nil, err
			}
		}
		id, err := compute.Required(basic, "XH")
		if err != nil {
			return nil, err
		}
		if _, err := compute.Required(basic, "XM"); err != nil {
			return nil, err
		}
		enrollment, ok := enrollmentIndex[id]
		if !ok {
			return nil, compute.ErrRecords
		}
		result = append(result, Pair{Basic: basic, Enrollment: enrollment})
	}
	return result, nil
}

type Related struct{ Departments, Campuses, Majors, Classes, Counselors, Contacts []source.SourceRow }
type Assembler struct {
	departments, campuses, majors, classes, counselors, contacts compute.Index
	times                                                        compute.Times
}

func NewAssembler(data Related, times compute.Times) (Assembler, error) {
	a := Assembler{times: times}
	for _, entry := range []struct {
		rows []source.SourceRow
		key  string
		dst  *compute.Index
	}{
		{data.Departments, "DWH", &a.departments}, {data.Campuses, "XQH", &a.campuses}, {data.Majors, "ZYH", &a.majors},
		{data.Classes, "BH", &a.classes}, {data.Counselors, "JGH", &a.counselors}, {data.Contacts, "XH", &a.contacts},
	} {
		index, err := compute.IndexRows(entry.rows, entry.key)
		if err != nil {
			return Assembler{}, err
		}
		*entry.dst = index
	}
	return a, nil
}

func text(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (a Assembler) Member(pair Pair) (result contract.Student, err error) {
	defer func() {
		if err != nil {
			result = contract.Student{}
		}
	}()
	if result.StudentNumber, err = compute.Required(pair.Basic, "XH"); err != nil {
		return
	}
	if result.Name, err = compute.Required(pair.Basic, "XM"); err != nil {
		return
	}
	enrollmentID, e := compute.Required(pair.Enrollment, "XH")
	if e != nil {
		err = e
		return
	}
	if enrollmentID != result.StudentNumber {
		err = compute.ErrRecords
		return
	}
	for _, field := range []struct {
		key string
		dst **string
	}{
		{"XSDQZTM", &result.StatusCode}, {"XJZTM", &result.EnrollmentStatusCode}, {"SFZX", &result.OnCampusFlag},
		{"SSXQH", &result.CampusCode}, {"ZYH", &result.MajorCode}, {"SZNJ", &result.Grade}, {"SZBH", &result.ClassCode},
	} {
		if *field.dst, err = compute.NullableText(pair.Enrollment, field.key); err != nil {
			return
		}
	}
	departmentCode, e := compute.Optional(pair.Enrollment, "YXSH")
	if e != nil {
		err = e
		return
	}
	department := a.departments[departmentCode]
	if result.Department, err = organizations.Department(departmentCode, department); err != nil {
		return
	}
	campus, major, class := a.campuses[text(result.CampusCode)], a.majors[text(result.MajorCode)], a.classes[text(result.ClassCode)]
	if result.CampusName, err = compute.NullableText(campus, "XQMC"); err != nil {
		return
	}
	if result.MajorName, err = compute.NullableText(major, "ZYMC"); err != nil {
		return
	}
	if result.ClassName, err = compute.NullableText(class, "BJMC"); err != nil {
		return
	}
	counselorCode, e := compute.Optional(class, "FDYJGH")
	if e != nil {
		err = e
		return
	}
	counselor := a.counselors[counselorCode]
	if counselorCode != "" {
		result.Counselor = &contract.Counselor{StaffNumber: counselorCode}
		if result.Counselor.Name, err = compute.NullableText(counselor, "XM"); err != nil {
			return
		}
	}
	contact := a.contacts[result.StudentNumber]
	if result.Email, err = compute.NullableText(contact, "DZXX"); err != nil {
		return
	}
	if result.Mobile, err = compute.NullableText(contact, "YDDH"); err != nil {
		return
	}
	var timestamps []*string
	for _, r := range []source.SourceRow{pair.Basic, pair.Enrollment, department, campus, major, class, counselor, contact} {
		at, e := compute.Text(r, "ETL_TIME")
		if e != nil {
			err = e
			return
		}
		timestamps = append(timestamps, at)
	}
	result.SourceUpdatedAt, err = a.times.Latest(timestamps...)
	return
}
