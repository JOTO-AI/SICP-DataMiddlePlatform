package teaching

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
)

func (s Service) Students(ctx context.Context, h read.Handle, req contract.TeachingClassStudentsRequest, synced time.Time) (contract.TeachingClassStudentsResponse, error) {
	fail := contract.TeachingClassStudentsResponse{}
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
	key := Key{AcademicYear: req.AcademicYear, Semester: req.Semester, Class: req.TeachingClassNumber, Course: req.PlannedCourseCode}
	rosters, err := (Membership{Source: s.Source}).Resolve(ctx, h, []Key{key}, b)
	if err != nil {
		return fail, err
	}
	roster, ok := rosters[key]
	if !ok {
		return fail, compute.ErrRecords
	}
	start, end := compute.PageBounds(len(roster.Members), page, size)
	items, err := s.StudentRows(ctx, h, roster, roster.Members[start:end], b)
	if err != nil {
		return fail, err
	}
	result := contract.TeachingClassStudentsResponse{TeachingClassNumber: req.TeachingClassNumber, PlannedCourseCode: req.PlannedCourseCode, AcademicYear: req.AcademicYear, Semester: req.Semester, Total: len(roster.Members), SyncedAt: *at, Items: items}
	if err := b.Check(ctx); err != nil {
		return fail, err
	}
	return result, nil
}
