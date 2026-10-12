package students

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/staff"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type Service struct {
	Source read.Matcher
	Times  compute.Times
	Clock  ports.Clock
	Limits read.Limits
}

func reserve(ctx context.Context, b *read.Budget, rows []source.SourceRow) error {
	for _, r := range rows {
		if err := b.Check(ctx); err != nil {
			return err
		}
		// Pair/index/DTO storage and decoded strings in addition to source payload.
		if err := b.Reserve(512); err != nil {
			return err
		}
		for _, raw := range r {
			if err := b.Reserve(len(raw)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s Service) Query(ctx context.Context, h read.Handle, req contract.StudentsRequest, synced time.Time) (contract.StudentsResponse, error) {
	fail := contract.StudentsResponse{}
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
	if req.StudentNumber != nil {
		filters = []source.Filter{{Column: "XH", Value: *req.StudentNumber}}
	}
	basics, err := read.Collect(ctx, s.Source, h, Table, filters, b)
	if err != nil {
		return fail, err
	}
	enrollments, err := read.Collect(ctx, s.Source, h, EnrollmentsTable, filters, b)
	if err != nil {
		return fail, err
	}
	for _, rows := range [][]source.SourceRow{basics, enrollments} {
		if err := reserve(ctx, b, rows); err != nil {
			return fail, err
		}
	}
	pairs, err := PairRecords(basics, enrollments, func() error { return b.Check(ctx) })
	if err != nil {
		return fail, err
	}
	// All pairs are validated before any optional filter can hide an orphan.
	load := func(rows []source.SourceRow, field, table, key string) ([]source.SourceRow, error) {
		keys := []string{}
		for _, r := range rows {
			if err := b.Check(ctx); err != nil {
				return nil, err
			}
			value, err := compute.Optional(r, field)
			if err != nil {
				return nil, err
			}
			if value != "" {
				if err := b.Reserve(len(value) + 16); err != nil {
					return nil, err
				}
				keys = append(keys, value)
			}
		}
		result, err := read.CollectKeys(ctx, s.Source, h, table, key, keys, b)
		if err != nil {
			return nil, err
		}
		if err := reserve(ctx, b, result); err != nil {
			return nil, err
		}
		return result, nil
	}
	var data Related
	for _, entry := range []struct {
		field, table, key string
		dst               *[]source.SourceRow
	}{
		{"YXSH", organizations.Table, "DWH", &data.Departments}, {"SSXQH", CampusesTable, "XQH", &data.Campuses},
		{"ZYH", MajorsTable, "ZYH", &data.Majors}, {"SZBH", ClassesTable, "BH", &data.Classes},
	} {
		*entry.dst, err = load(enrollments, entry.field, entry.table, entry.key)
		if err != nil {
			return fail, err
		}
	}
	data.Counselors, err = load(data.Classes, "FDYJGH", staff.Table, "JGH")
	if err != nil {
		return fail, err
	}
	a, err := NewAssembler(data, s.Times)
	if err != nil {
		return fail, err
	}
	selected := make([]Pair, 0)
	for _, pair := range pairs {
		if err := b.Check(ctx); err != nil {
			return fail, err
		}
		item, err := a.Member(pair)
		if err != nil {
			return fail, err
		}
		if Match(item, req) {
			selected = append(selected, pair)
		}
	}
	// Scan guarantees C primary-key order; PairRecords and filtering preserve it.
	start, end := compute.PageBounds(len(selected), page, size)
	pageBasics := make([]source.SourceRow, 0, end-start)
	for _, pair := range selected[start:end] {
		pageBasics = append(pageBasics, pair.Basic)
	}
	// Contacts affect neither identity completeness nor filters. Fetch only the
	// requested page, avoiding a full-population contact scan on every page.
	data.Contacts, err = load(pageBasics, "XH", ContactsTable, "XH")
	if err != nil {
		return fail, err
	}
	contactIndex, err := compute.IndexRows(data.Contacts, "XH")
	if err != nil {
		return fail, err
	}
	a.contacts = contactIndex
	result := contract.StudentsResponse{Total: len(selected), SyncedAt: *at, Items: contract.Array[contract.Student]{}}
	for _, pair := range selected[start:end] {
		if err := b.Check(ctx); err != nil {
			return fail, err
		}
		item, err := a.Member(pair)
		if err != nil {
			return fail, err
		}
		result.Items = append(result.Items, item)
	}
	if err := b.Check(ctx); err != nil {
		return fail, err
	}
	return result, nil
}
