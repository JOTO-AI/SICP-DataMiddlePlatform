package grades

import (
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func (a Assembler) StudentPage(rows []source.SourceRow, req contract.StudentGradesRequest, syncedAt time.Time) (contract.GradesResponse, error) {
	selected, err := a.Student(rows, req)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	return a.page(selected, req.GradeFilters, syncedAt)
}

func (a Assembler) BatchPage(rows []source.SourceRow, req contract.BatchGradesRequest, syncedAt time.Time) (contract.GradesResponse, error) {
	selected, err := a.Batch(rows, req)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	return a.page(selected, req.GradeFilters, syncedAt)
}

func (a Assembler) page(selected []contract.Grade, filters contract.GradeFilters, syncedAt time.Time) (contract.GradesResponse, error) {
	page, size := 1, 100
	if filters.Page != nil {
		page = *filters.Page
	}
	if filters.PageSize != nil {
		size = *filters.PageSize
	}
	total, items, err := compute.Grades(selected, page, size)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	synced, err := a.times.Instant(syncedAt)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	return contract.GradesResponse{Total: total, SyncedAt: *synced, Items: items}, nil
}
