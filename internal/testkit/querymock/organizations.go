package querymock

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"testing"
)

const organizationRoot = `[{"organization_code":"10001","organization_name":"虚构测试学院","parent_organization_code":null,"category_code":"1","category_name":"教学院系","is_active":true,"source_updated_at":"2026-08-05T07:50:00+08:00"}]`
const organizationInactive = `[{"organization_code":"00D4","organization_name":"虚构单位4","parent_organization_code":"00D2","category_code":"5","category_name":"行政机构","is_active":false,"source_updated_at":"2026-08-05T07:50:00+08:00"}]`

func Organizations(t *testing.T, q Query) {
	t.Helper()
	exact(t, q, contract.Organizations, `{"organization_code":"10001"}`, organizationRoot)
	exact(t, q, contract.Organizations, `{"is_active":false}`, organizationInactive)
	exact(t, q, contract.Organizations, `{"organization_name":"单位4","category_codes":["5"],"parent_organization_code":"00D2","is_active":false}`, organizationInactive)
	exact(t, q, contract.Organizations, `{"organization_code":"00D4","is_active":true}`, `[]`)
	pages(t, q, contract.Organizations, `{}`, "organization_code", []string{"00D1", "00D2", "00D3", "00D4", "10001"})
	pages(t, q, contract.Organizations, `{"parent_organization_code":"10001"}`, "organization_code", []string{"00D1", "00D2"})
	pages(t, q, contract.Organizations, `{"category_codes":["1"],"is_active":true}`, "organization_code", []string{"00D1", "00D3", "10001"})
}
