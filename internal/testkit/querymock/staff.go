package querymock

import (
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

const staffOne = `[{"staff_number":"001","name":"虚构教师001","status_code":"11","department":{"organization_code":"00D1","organization_name":"虚构单位1","parent_organization_code":"10001","category_code":"1","category_name":"教学院系","is_active":true},"email":"teacher001@campus.example","mobile":"00090000001","source_updated_at":"2026-08-05T07:50:00+08:00"}]`
const staffTwo = `[{"staff_number":"1001","name":"虚构教师002","status_code":"11","department":{"organization_code":"00D2","organization_name":"虚构单位2","parent_organization_code":"10001","category_code":"5","category_name":"行政机构","is_active":true},"email":" teacher002@campus.example ","mobile":"00090000001","source_updated_at":"2026-08-05T07:50:00+08:00"}]`

func Staff(t *testing.T, q Query) {
	t.Helper()
	exact(t, q, contract.Staff, `{"staff_number":"001"}`, staffOne)
	exact(t, q, contract.Staff, `{"staff_number":"1001","name":"教师002","department_code":"00D2","department_name":"单位2","status_codes":["11"]}`, staffTwo)
	exact(t, q, contract.Staff, `{"staff_number":"001","status_codes":["unknown"]}`, `[]`)
	exact(t, q, contract.Staff, `{"staff_number":"1"}`, `[]`)
	pages(t, q, contract.Staff, `{}`, "staff_number", []string{"001", "1001", "T00003", "T00004", "T00005", "T00006"})
	pages(t, q, contract.Staff, `{"department_code":"00D1","status_codes":["11"]}`, "staff_number", []string{"001", "T00003", "T00005"})
}
