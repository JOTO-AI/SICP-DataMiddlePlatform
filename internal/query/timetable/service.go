package timetable

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

func (s Service) Staff(ctx context.Context, h read.Handle, req contract.StaffTimetableRequest, synced time.Time) (out contract.StaffTimetableResponse, err error) {
	defer func() {
		if err != nil {
			out = contract.StaffTimetableResponse{}
		}
	}()
	if _, err = StaffMatches(nil, req); err != nil {
		return
	}
	b, err := read.NewBudget(s.Limits, s.Clock)
	if err != nil {
		return out, err
	}
	// Teacher IDs are delimited source values, not SQL substring predicates.
	matches := []Match{}
	err = read.Scan(ctx, s.Source, h, SchedulesTable, []source.Filter{{Column: "KKXND", Value: req.AcademicYear}, {Column: "KKXQM", Value: req.Semester}}, b, func(row source.SourceRow) error {
		found, err := staffMatchesChecked([]source.SourceRow{row}, req, func() error { return b.Check(ctx) })
		if err != nil {
			return err
		}
		if len(found) > 0 {
			if err = b.Retain(row); err != nil {
				return err
			}
			matches = append(matches, found...)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	return s.staffResponse(ctx, h, req, synced, matches, b)
}

func (s Service) staffResponse(ctx context.Context, h read.Handle, req contract.StaffTimetableRequest, synced time.Time, matches []Match, b *read.Budget) (contract.StaffTimetableResponse, error) {
	a, err := s.assembler(ctx, h, matches, b)
	if err != nil {
		return contract.StaffTimetableResponse{}, err
	}
	entries, err := a.entriesChecked(matches, func() error { return b.Check(ctx) })
	if err != nil {
		return contract.StaffTimetableResponse{}, err
	}
	at, err := s.Times.Instant(synced)
	if err != nil {
		return contract.StaffTimetableResponse{}, err
	}
	if err := b.Check(ctx); err != nil {
		return contract.StaffTimetableResponse{}, err
	}
	return contract.StaffTimetableResponse{StaffNumber: req.StaffNumber, AcademicYear: req.AcademicYear, Semester: req.Semester, SyncedAt: *at, Items: FilterStaff(entries, req)}, nil
}

func (s Service) Student(ctx context.Context, h read.Handle, req contract.StudentTimetableRequest, synced time.Time) (out contract.StudentTimetableResponse, err error) {
	defer func() {
		if err != nil {
			out = contract.StudentTimetableResponse{}
		}
	}()
	if _, err = StudentMatches(nil, nil, req); err != nil {
		return
	}
	b, err := read.NewBudget(s.Limits, s.Clock)
	if err != nil {
		return out, err
	}
	relations, err := read.Collect(ctx, s.Source, h, RelationsTable, []source.Filter{{Column: "XH", Value: req.StudentNumber}}, b)
	if err != nil {
		return out, err
	}
	keys := map[relationKey]bool{}
	for _, r := range relations {
		if err = b.Check(ctx); err != nil {
			return
		}
		class, e := compute.Required(r, "JXBH")
		if e != nil {
			return out, e
		}
		course, e := compute.Optional(r, "JHKCH")
		if e != nil {
			return out, e
		}
		if course == "" {
			continue
		}
		keys[relationKey{class, course}] = true
	}
	ordered := make([]relationKey, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].class != ordered[j].class {
			return ordered[i].class < ordered[j].class
		}
		return ordered[i].course < ordered[j].course
	})
	schedules := []source.SourceRow{}
	for _, key := range ordered {
		// Deliberately no term filter: member rows cannot disambiguate reused keys.
		found, e := read.Collect(ctx, s.Source, h, SchedulesTable, []source.Filter{{Column: "JXBH", Value: key.class}, {Column: "JHKCH", Value: key.course}}, b)
		if e != nil {
			return out, e
		}
		schedules = append(schedules, found...)
	}
	matches, err := studentMatchesChecked(relations, schedules, req, func() error { return b.Check(ctx) })
	if err != nil {
		return out, err
	}
	a, err := s.assembler(ctx, h, matches, b)
	if err != nil {
		return out, err
	}
	entries, err := a.entriesChecked(matches, func() error { return b.Check(ctx) })
	if err != nil {
		return out, err
	}
	at, err := s.Times.Instant(synced)
	if err != nil {
		return out, err
	}
	out = contract.StudentTimetableResponse{StudentNumber: req.StudentNumber, AcademicYear: req.AcademicYear, Semester: req.Semester, SyncedAt: *at, Items: FilterCourse(entries, req.CourseQuery)}
	err = b.Check(ctx)
	return
}

func (s Service) assembler(ctx context.Context, h read.Handle, matches []Match, b *read.Budget) (Assembler, error) {
	keys := map[string]bool{}
	for _, m := range matches {
		if err := b.Check(ctx); err != nil {
			return Assembler{}, err
		}
		key, err := compute.Optional(m.Schedule, "JHKCH")
		if err != nil {
			return Assembler{}, err
		}
		if key != "" {
			keys[key] = true
		}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	courses := []source.SourceRow{}
	for _, key := range ordered {
		found, err := read.Collect(ctx, s.Source, h, CoursesTable, []source.Filter{{Column: "KCH", Value: key}}, b)
		if err != nil {
			return Assembler{}, err
		}
		courses = append(courses, found...)
	}
	return NewAssembler(courses, s.Times)
}
