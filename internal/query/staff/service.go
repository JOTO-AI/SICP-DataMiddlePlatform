package staff

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type Service struct {
	Source read.Matcher
	Times  compute.Times
	Clock  ports.Clock
	Limits read.Limits
}

func Match(item contract.StaffMember, req contract.StaffRequest) bool {
	if req.StaffNumber != nil && item.StaffNumber != *req.StaffNumber {
		return false
	}
	if req.Name != nil && !strings.Contains(item.Name, *req.Name) {
		return false
	}
	if req.DepartmentCode != nil && (item.Department == nil || item.Department.OrganizationCode != *req.DepartmentCode) {
		return false
	}
	if req.DepartmentName != nil && (item.Department == nil || item.Department.OrganizationName == nil || !strings.Contains(*item.Department.OrganizationName, *req.DepartmentName)) {
		return false
	}
	if req.StatusCodes != nil {
		if item.StatusCode == nil {
			return false
		}
		for _, code := range req.StatusCodes {
			if code == *item.StatusCode {
				return true
			}
		}
		return false
	}
	return true
}

func (s Service) Query(ctx context.Context, h read.Handle, req contract.StaffRequest, synced time.Time) (contract.StaffResponse, error) {
	fail := contract.StaffResponse{}
	page, size, err := compute.Pagination(req.PaginationRequest, 1000)
	if err != nil {
		return fail, err
	}
	at, err := s.Times.Instant(synced)
	if err != nil {
		return fail, err
	}
	b, err := read.NewBudget(s.Limits, s.Clock)
	if err != nil {
		return fail, err
	}
	var filters []source.Filter
	if req.StaffNumber != nil {
		filters = []source.Filter{{Column: "JGH", Value: *req.StaffNumber}}
	}
	rows, err := read.Collect(ctx, s.Source, h, Table, filters, b)
	if err != nil {
		return fail, err
	}
	ids, departmentIDs := []string{}, []string{}
	for _, r := range rows {
		if err := b.Check(ctx); err != nil {
			return fail, err
		}
		id, err := compute.Required(r, "JGH")
		if err != nil {
			return fail, err
		}
		if _, err := compute.Required(r, "XM"); err != nil {
			return fail, err
		}
		dep, err := compute.Optional(r, "DWH")
		if err != nil {
			return fail, err
		}
		if err := b.Reserve(len(id) + len(dep) + 64); err != nil {
			return fail, err
		}
		ids = append(ids, id)
		if dep != "" {
			departmentIDs = append(departmentIDs, dep)
		}
	}
	contacts, err := read.CollectKeys(ctx, s.Source, h, ContactsTable, "JGH", ids, b)
	if err != nil {
		return fail, err
	}
	departments, err := read.CollectKeys(ctx, s.Source, h, organizations.Table, "DWH", departmentIDs, b)
	if err != nil {
		return fail, err
	}
	for _, group := range [][]source.SourceRow{rows, contacts, departments} {
		for _, r := range group {
			if err := b.Check(ctx); err != nil {
				return fail, err
			}
			if err := b.Reserve(768); err != nil {
				return fail, err
			}
			for _, raw := range r {
				if err := b.Reserve(len(raw)); err != nil {
					return fail, err
				}
			}
		}
	}
	a, err := NewAssembler(Related{Contacts: contacts, Departments: departments}, s.Times)
	if err != nil {
		return fail, err
	}
	items := contract.Array[contract.StaffMember]{}
	for _, r := range rows {
		if err := b.Check(ctx); err != nil {
			return fail, err
		}
		item, err := a.Member(r)
		if err != nil {
			return fail, err
		}
		if Match(item, req) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StaffNumber < items[j].StaffNumber })
	start, end := compute.PageBounds(len(items), page, size)
	result := contract.StaffResponse{Total: len(items), SyncedAt: *at, Items: append(contract.Array[contract.StaffMember]{}, items[start:end]...)}
	if err := b.Check(ctx); err != nil {
		return fail, err
	}
	return result, nil
}
