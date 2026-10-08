package timetable

import (
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func StaffMatches(schedules []source.SourceRow, req contract.StaffTimetableRequest) ([]Match, error) {
	return staffMatchesChecked(schedules, req, nil)
}

func (a Assembler) Staff(schedules []source.SourceRow, req contract.StaffTimetableRequest) ([]contract.TimetableEntry, error) {
	matches, err := StaffMatches(schedules, req)
	if err != nil {
		return nil, err
	}
	entries, err := a.Entries(matches)
	if err != nil {
		return nil, err
	}
	return FilterStaff(entries, req), nil
}

func staffMatchesChecked(schedules []source.SourceRow, req contract.StaffTimetableRequest, check func() error) ([]Match, error) {
	if strings.TrimSpace(req.StaffNumber) == "" || !validTerm(req.AcademicYear, req.Semester) {
		return nil, compute.ErrValue
	}
	out := []Match{}
	for _, r := range schedules {
		if check != nil {
			if err := check(); err != nil {
				return nil, err
			}
		}
		teachers, err := staffTeachers(r, req.AcademicYear, req.Semester)
		if err != nil {
			return nil, err
		}
		if teachers.Contains(req.StaffNumber) {
			out = append(out, Match{Schedule: r})
		}
	}
	return out, nil
}

// The sequential service and grouped validation share exact term/token rules,
// including errors on malformed rows belonging to another requested teacher.
func staffTeachers(r source.SourceRow, year, semester string) (Teachers, error) {
	y, err := compute.Required(r, "KKXND")
	if err != nil {
		return Teachers{}, err
	}
	s, err := compute.Required(r, "KKXQM")
	if err != nil {
		return Teachers{}, err
	}
	if !validTerm(y, s) {
		return Teachers{}, compute.ErrValue
	}
	if y != year || s != semester {
		return Teachers{}, nil
	}
	return ParseTeachers(r)
}
