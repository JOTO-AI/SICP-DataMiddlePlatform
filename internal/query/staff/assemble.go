package staff

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const Table = "dwd_jzgjcsjzlb"
const ContactsTable = "dwd_grtxfszlb"

type Related struct{ Contacts, Departments []source.SourceRow }
type Assembler struct {
	contacts, departments compute.Index
	times                 compute.Times
}

// Related records are current published whole-row winners. Query code must
// never merge older nonempty contact fields into a newer record.
func NewAssembler(data Related, times compute.Times) (Assembler, error) {
	contacts, err := compute.IndexRows(data.Contacts, "JGH")
	if err != nil {
		return Assembler{}, err
	}
	departments, err := compute.IndexRows(data.Departments, "DWH")
	if err != nil {
		return Assembler{}, err
	}
	return Assembler{contacts: contacts, departments: departments, times: times}, nil
}

func (a Assembler) Member(row source.SourceRow) (result contract.StaffMember, err error) {
	defer func() {
		if err != nil {
			result = contract.StaffMember{}
		}
	}()
	if result.StaffNumber, err = compute.Required(row, "JGH"); err != nil {
		return
	}
	if result.Name, err = compute.Required(row, "XM"); err != nil {
		return
	}
	if result.StatusCode, err = compute.NullableText(row, "DQZTM"); err != nil {
		return
	}
	code, err := compute.Optional(row, "DWH")
	if err != nil {
		return
	}
	department := a.departments[code]
	if result.Department, err = organizations.Department(code, department); err != nil {
		return
	}
	contact := a.contacts[result.StaffNumber]
	if result.Email, err = compute.NullableText(contact, "DZXX"); err != nil {
		return
	}
	if result.Mobile, err = compute.NullableText(contact, "YDDH"); err != nil {
		return
	}
	var timestamps []*string
	for _, r := range []source.SourceRow{row, contact, department} {
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
