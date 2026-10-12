package organizations

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
)

type Service struct {
	Source read.Matcher
	Times  compute.Times
	Clock  ports.Clock
	Limits read.Limits
}

func Match(item contract.Organization, req contract.OrganizationsRequest) bool {
	if req.OrganizationCode != nil && item.OrganizationCode != *req.OrganizationCode {
		return false
	}
	if req.OrganizationName != nil && !strings.Contains(item.OrganizationName, *req.OrganizationName) {
		return false
	}
	if req.ParentOrganizationCode != nil && (item.ParentOrganizationCode == nil || *item.ParentOrganizationCode != *req.ParentOrganizationCode) {
		return false
	}
	if req.IsActive != nil && item.IsActive != *req.IsActive {
		return false
	}
	if req.CategoryCodes != nil {
		if item.CategoryCode == nil {
			return false
		}
		for _, code := range req.CategoryCodes {
			if code == *item.CategoryCode {
				return true
			}
		}
		return false
	}
	return true
}

func (s Service) Query(ctx context.Context, h read.Handle, req contract.OrganizationsRequest, synced time.Time) (contract.OrganizationsResponse, error) {
	fail := contract.OrganizationsResponse{}
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
	// No filter pushdown: any node can invalidate the complete graph.
	rows, err := read.Collect(ctx, s.Source, h, Table, nil, b)
	if err != nil {
		return fail, err
	}
	for _, row := range rows {
		if err := b.Check(ctx); err != nil {
			return fail, err
		}
		// DTOs, pointers, graph indexes/path and decoded source strings.
		if err := b.Reserve(768); err != nil {
			return fail, err
		}
		for _, raw := range row {
			if err := b.Reserve(len(raw)); err != nil {
				return fail, err
			}
		}
	}
	items, err := Assemble(rows, s.Times, func() error { return b.Check(ctx) })
	if err != nil {
		return fail, err
	}
	selected := items[:0]
	for _, item := range items {
		if err := b.Check(ctx); err != nil {
			return fail, err
		}
		if Match(item, req) {
			selected = append(selected, item)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].OrganizationCode < selected[j].OrganizationCode })
	start, end := compute.PageBounds(len(selected), page, size)
	result := contract.OrganizationsResponse{Total: len(selected), SyncedAt: *at, Items: append(contract.Array[contract.Organization]{}, selected[start:end]...)}
	if err := b.Check(ctx); err != nil {
		return fail, err
	}
	return result, nil
}
