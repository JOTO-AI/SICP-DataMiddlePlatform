package teaching

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/students"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// StudentRows enriches only the selected actual members. This intentionally does
// not use the strict profile pairing rule or infer membership from class codes.
func (s Service) StudentRows(ctx context.Context, h read.Handle, roster Roster, selected []Member, b *read.Budget) ([]contract.TeachingClassStudent, error) {
	ids := []string{}
	for _, member := range selected {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		if err := b.Reserve(len(member.StudentNumber) + 16); err != nil {
			return nil, err
		}
		ids = append(ids, member.StudentNumber)
	}
	load := func(table, key string, ids []string) (compute.Index, error) {
		index, err := read.LookupMany(ctx, s.Source, h, table, key, ids, b)
		if err != nil {
			return nil, err
		}
		for _, row := range index {
			if err := reserveRows(ctx, b, []source.SourceRow{row}); err != nil {
				return nil, err
			}
		}
		return index, nil
	}
	basics, err := load(students.Table, "XH", ids)
	if err != nil {
		return nil, err
	}
	enrollments, err := load(students.EnrollmentsTable, "XH", ids)
	if err != nil {
		return nil, err
	}
	departmentIDs, classIDs := []string{}, []string{}
	for _, row := range enrollments {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		for _, entry := range []struct {
			field string
			dst   *[]string
		}{{"YXSH", &departmentIDs}, {"SZBH", &classIDs}} {
			id, err := compute.Optional(row, entry.field)
			if err != nil {
				return nil, err
			}
			if id != "" {
				if err := b.Reserve(len(id) + 16); err != nil {
					return nil, err
				}
				*entry.dst = append(*entry.dst, id)
			}
		}
	}
	departments, err := load(organizations.Table, "DWH", departmentIDs)
	if err != nil {
		return nil, err
	}
	classes, err := load(students.ClassesTable, "BH", classIDs)
	if err != nil {
		return nil, err
	}
	latestOf := func(latest *string, row source.SourceRow) (*string, error) {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		raw, err := compute.Text(row, "ETL_TIME")
		if err != nil {
			return nil, err
		}
		return s.Times.Latest(latest, raw)
	}
	var scheduleTime *string
	for _, row := range roster.Schedules {
		scheduleTime, err = latestOf(scheduleTime, row)
		if err != nil {
			return nil, err
		}
	}
	result := []contract.TeachingClassStudent{}
	for _, member := range selected {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		item := contract.TeachingClassStudent{StudentNumber: member.StudentNumber, SourceUpdatedAt: scheduleTime}
		basic, enrollment := basics[member.StudentNumber], enrollments[member.StudentNumber]
		item.Name, err = compute.NullableText(basic, "XM")
		if err != nil {
			return nil, err
		}
		item.DepartmentCode, err = compute.NullableText(enrollment, "YXSH")
		if err != nil {
			return nil, err
		}
		item.ClassCode, err = compute.NullableText(enrollment, "SZBH")
		if err != nil {
			return nil, err
		}
		var department, class source.SourceRow
		if item.DepartmentCode != nil {
			department = departments[*item.DepartmentCode]
		}
		if item.ClassCode != nil {
			class = classes[*item.ClassCode]
		}
		item.DepartmentName, err = compute.NullableText(department, "DWMC")
		if err != nil {
			return nil, err
		}
		item.ClassName, err = compute.NullableText(class, "BJMC")
		if err != nil {
			return nil, err
		}
		for _, row := range []source.SourceRow{basic, enrollment, department, class} {
			item.SourceUpdatedAt, err = latestOf(item.SourceUpdatedAt, row)
			if err != nil {
				return nil, err
			}
		}
		for _, row := range member.Relations {
			item.SourceUpdatedAt, err = latestOf(item.SourceUpdatedAt, row)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, item)
	}
	if err := b.Check(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
