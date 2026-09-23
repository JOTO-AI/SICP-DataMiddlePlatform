package timetable

import (
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// MatchCourse matches an entire code or a literal name substring. A code
// substring alone cannot establish a match and OR does not duplicate entries.
func MatchCourse(entry contract.TimetableEntry, query *string) bool {
	return query == nil || entry.CourseCode == *query || entry.CourseName != nil && strings.Contains(*entry.CourseName, *query)
}

func FilterCourse(entries []contract.TimetableEntry, query *string) []contract.TimetableEntry {
	out := []contract.TimetableEntry{}
	for _, entry := range entries {
		if MatchCourse(entry, query) {
			out = append(out, entry)
		}
	}
	return out
}

func FilterStaff(entries []contract.TimetableEntry, req contract.StaffTimetableRequest) []contract.TimetableEntry {
	out := []contract.TimetableEntry{}
	for _, entry := range entries {
		if req.TeachingClassNumber != nil && entry.TeachingClassNumber != *req.TeachingClassNumber {
			continue
		}
		if MatchCourse(entry, req.CourseQuery) {
			out = append(out, entry)
		}
	}
	return out
}

func (a Assembler) Student(relations, schedules []source.SourceRow, req contract.StudentTimetableRequest) ([]contract.TimetableEntry, error) {
	matches, err := StudentMatches(relations, schedules, req)
	if err != nil {
		return nil, err
	}
	entries, err := a.Entries(matches)
	if err != nil {
		return nil, err
	}
	return FilterCourse(entries, req.CourseQuery), nil
}
