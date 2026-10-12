package teaching

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
)

func (s Service) Staff(ctx context.Context, h read.Handle, req contract.StaffTeachingClassesRequest, synced time.Time) (contract.StaffTeachingClassesResponse, error) {
	fail := contract.StaffTeachingClassesResponse{}
	page, size, err := compute.Pagination(req.PaginationRequest, 100)
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
	items, err := s.StaffClasses(ctx, h, req, b)
	if err != nil {
		return fail, err
	}
	// Aggregation orders the common term by class and planned course in C order.
	start, end := compute.PageBounds(len(items), page, size)
	result := contract.StaffTeachingClassesResponse{StaffNumber: req.StaffNumber, AcademicYear: req.AcademicYear, Semester: req.Semester, Total: len(items), SyncedAt: *at, Items: append(contract.Array[contract.TeachingClass]{}, items[start:end]...)}
	if err := b.Check(ctx); err != nil {
		return fail, err
	}
	return result, nil
}
