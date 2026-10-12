package teaching

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/staff"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type Service struct {
	Source read.Matcher
	Times  compute.Times
	Clock  ports.Clock
	Limits read.Limits
}

// Class builds a response from a validated full group and its shared roster.
func Class(group Group, roster Roster, courses, teachers compute.Index, times compute.Times, check func() error) (contract.TeachingClass, error) {
	fail := contract.TeachingClass{}
	result := contract.TeachingClass{TeachingClassNumber: group.Key.Class, TeachingClassName: group.Name, PlannedCourseCode: group.Key.Course, CourseCode: group.Key.Course, StudentCount: len(roster.Members), Teachers: contract.Array[contract.TeachingClassTeacher]{}}
	course := courses[group.Key.Course]
	var err error
	result.CourseName, err = compute.NullableText(course, "KCMC")
	if err != nil {
		return fail, err
	}
	// Fold timestamps incrementally so relation count does not allocate another
	// full pointer list. All duplicate membership rows still contribute.
	var latest *string
	addTime := func(row source.SourceRow) error {
		if check != nil {
			if err := check(); err != nil {
				return err
			}
		}
		raw, err := compute.Text(row, "ETL_TIME")
		if err != nil {
			return err
		}
		latest, err = times.Latest(latest, raw)
		return err
	}
	if err := addTime(course); err != nil {
		return fail, err
	}
	for _, id := range group.TeacherNumbers {
		row := teachers[id]
		name, err := compute.NullableText(row, "XM")
		if err != nil {
			return fail, err
		}
		result.Teachers = append(result.Teachers, contract.TeachingClassTeacher{StaffNumber: id, Name: name})
		if err := addTime(row); err != nil {
			return fail, err
		}
	}
	for _, row := range roster.Schedules {
		if err := addTime(row); err != nil {
			return fail, err
		}
	}
	for _, member := range roster.Members {
		for _, row := range member.Relations {
			if err := addTime(row); err != nil {
				return fail, err
			}
		}
	}
	result.SourceUpdatedAt = latest
	return result, nil
}

// StaffClasses validates every candidate before pagination. No staff profile is
// required to discover a teaching assignment from the schedule source.
func (s Service) StaffClasses(ctx context.Context, h read.Handle, req contract.StaffTeachingClassesRequest, b *read.Budget) ([]contract.TeachingClass, error) {
	if _, err := ForTeacher(nil, req, nil); err != nil {
		return nil, err
	}
	filters := []source.Filter{{Column: "KKXND", Value: req.AcademicYear}, {Column: "KKXQM", Value: req.Semester}}
	if req.CourseCode != nil {
		filters = append(filters, source.Filter{Column: "JHKCH", Value: *req.CourseCode})
	}
	schedules, err := read.Collect(ctx, s.Source, h, timetable.SchedulesTable, filters, b)
	if err != nil {
		return nil, err
	}
	if err := reserveRows(ctx, b, schedules); err != nil {
		return nil, err
	}
	// Account token maps separately from source bytes before aggregation.
	for _, row := range schedules {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		_, present, err := Identity(row)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		ids, err := timetable.ParseTeacherIDs(row)
		if err != nil {
			return nil, err
		}
		for range ids {
			if err := b.Reserve(192); err != nil {
				return nil, err
			}
		}
	}
	groups, err := ForTeacher(schedules, req, func() error { return b.Check(ctx) })
	if err != nil {
		return nil, err
	}
	keys := []Key{}
	courseKeys, teacherKeys := []string{}, []string{}
	for _, group := range groups {
		if err := b.Reserve(len(group.Key.Course) + 128); err != nil {
			return nil, err
		}
		keys = append(keys, group.Key)
		courseKeys = append(courseKeys, group.Key.Course)
		teacherKeys = append(teacherKeys, group.TeacherNumbers...)
	}
	rosters, err := (Membership{Source: s.Source}).Resolve(ctx, h, keys, b)
	if err != nil {
		return nil, err
	}
	courses, err := read.LookupMany(ctx, s.Source, h, timetable.CoursesTable, "KCH", courseKeys, b)
	if err != nil {
		return nil, err
	}
	teachers, err := read.LookupMany(ctx, s.Source, h, staff.Table, "JGH", teacherKeys, b)
	if err != nil {
		return nil, err
	}
	for _, index := range []compute.Index{courses, teachers} {
		for _, row := range index {
			if err := reserveRows(ctx, b, []source.SourceRow{row}); err != nil {
				return nil, err
			}
		}
	}
	result := []contract.TeachingClass{}
	for _, group := range groups {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		roster, ok := rosters[group.Key]
		if !ok {
			return nil, compute.ErrRecords
		}
		item, err := Class(group, roster, courses, teachers, s.Times, func() error { return b.Check(ctx) })
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := b.Check(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
