package grades

import (
	"context"
	"sort"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type Service struct {
	Source read.Matcher
	Times  compute.Times
	Clock  ports.Clock
	Limits read.Limits
}

func (s Service) Batch(ctx context.Context, h read.Handle, req contract.BatchGradesRequest, synced time.Time) (contract.GradesResponse, error) {
	// Validate explicit range even if the source returns no records.
	if _, err := BatchScope(nil, req); err != nil {
		return contract.GradesResponse{}, err
	}
	b, err := read.NewBudget(s.Limits, s.Clock)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	filters := []source.Filter{}
	if req.AcademicYear != "all" {
		filters = append(filters, source.Filter{Column: "XN", Value: req.AcademicYear})
	}
	if req.Semester != nil {
		filters = append(filters, source.Filter{Column: "XQM", Value: *req.Semester})
	}
	rows := []source.SourceRow{}
	if req.StudentNumbers == nil {
		rows, err = read.Collect(ctx, s.Source, h, GradesTable, filters, b)
	} else {
		set := map[string]bool{}
		for _, id := range req.StudentNumbers {
			set[id] = true
		}
		ids := make([]string, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			bound := append(append([]source.Filter(nil), filters...), source.Filter{Column: "XH", Value: id})
			var found []source.SourceRow
			found, err = read.Collect(ctx, s.Source, h, GradesTable, bound, b)
			if err != nil {
				break
			}
			rows = append(rows, found...)
		}
	}
	if err != nil {
		return contract.GradesResponse{}, err
	}
	scoped, err := BatchScope(rows, req)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	a, err := s.assembler(ctx, h, scoped, b)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	selected, err := a.batchChecked(scoped, req, func() error { return b.Check(ctx) })
	if err != nil {
		return contract.GradesResponse{}, err
	}
	result, err := a.page(selected, req.GradeFilters, synced)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	if err = b.Check(ctx); err != nil {
		return contract.GradesResponse{}, err
	}
	return result, nil
}

func (s Service) Student(ctx context.Context, h read.Handle, req contract.StudentGradesRequest, synced time.Time) (contract.GradesResponse, error) {
	b, err := read.NewBudget(s.Limits, s.Clock)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	rows, err := read.Collect(ctx, s.Source, h, GradesTable, []source.Filter{{Column: "XH", Value: req.StudentNumber}}, b)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	scoped, err := StudentScope(rows, req)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	a, err := s.assembler(ctx, h, scoped, b)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	selected, err := a.studentChecked(scoped, req, func() error { return b.Check(ctx) })
	if err != nil {
		return contract.GradesResponse{}, err
	}
	result, err := a.page(selected, req.GradeFilters, synced)
	if err != nil {
		return contract.GradesResponse{}, err
	}
	if err = b.Check(ctx); err != nil {
		return contract.GradesResponse{}, err
	}
	return result, nil
}

func (s Service) assembler(ctx context.Context, h read.Handle, rows []source.SourceRow, b *read.Budget) (Assembler, error) {
	var related Related
	load := func(input []source.SourceRow, field, table, pk string) ([]source.SourceRow, error) {
		keys := map[string]bool{}
		for _, row := range input {
			v, err := compute.Text(row, field)
			if err != nil {
				return nil, err
			}
			if v != nil && *v != "" {
				keys[*v] = true
			}
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		result := []source.SourceRow{}
		for _, key := range ordered {
			found, err := read.Collect(ctx, s.Source, h, table, []source.Filter{{Column: pk, Value: key}}, b)
			if err != nil {
				return nil, err
			}
			result = append(result, found...)
		}
		return result, nil
	}
	var err error
	if related.Students, err = load(rows, "XH", StudentsTable, "XH"); err != nil {
		return Assembler{}, err
	}
	if related.Enrollments, err = load(rows, "XH", EnrollmentsTable, "XH"); err != nil {
		return Assembler{}, err
	}
	if related.Courses, err = load(rows, "KCH", CoursesTable, "KCH"); err != nil {
		return Assembler{}, err
	}
	if related.Classes, err = load(related.Enrollments, "SZBH", ClassesTable, "BH"); err != nil {
		return Assembler{}, err
	}
	// Combine both kinds of department references so a shared department is
	// loaded once and does not look like a duplicate consistency violation.
	refs := []source.SourceRow{}
	for _, e := range []struct {
		rows  []source.SourceRow
		field string
	}{{related.Enrollments, "YXSH"}, {related.Courses, "KKXY"}} {
		for _, row := range e.rows {
			refs = append(refs, source.SourceRow{"DWH": row[e.field]})
		}
	}
	if related.Departments, err = load(refs, "DWH", DepartmentsTable, "DWH"); err != nil {
		return Assembler{}, err
	}
	return NewAssembler(related, s.Times)
}
