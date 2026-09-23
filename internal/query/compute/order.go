package compute

import (
	"errors"
	"sort"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

var ErrRecords = errors.New("invalid or duplicate query record identity")

// Grades receives the complete filtered set. Sorting is on a private slice,
// total counts records, and distinct grade IDs for one course stay separate.
func Grades(rows []contract.Grade, page, pageSize int) (int, []contract.Grade, error) {
	if page < 1 || page > 1000000 || pageSize < 1 || pageSize > 1000 {
		return 0, nil, ErrRecords
	}
	ordered := append([]contract.Grade(nil), rows...)
	seen := map[string]bool{}
	for _, row := range ordered {
		if row.GradeID == "" || row.Student.StudentNumber == "" || seen[row.GradeID] {
			return 0, nil, ErrRecords
		}
		seen[row.GradeID] = true
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.Student.StudentNumber != b.Student.StudentNumber {
			return a.Student.StudentNumber < b.Student.StudentNumber
		}
		return a.GradeID < b.GradeID
	})
	total := len(ordered)
	start := int64(page-1) * int64(pageSize)
	if start >= int64(total) {
		return total, []contract.Grade{}, nil
	}
	end := start + int64(pageSize)
	if end > int64(total) {
		end = int64(total)
	}
	return total, ordered[int(start):int(end)], nil
}

type Schedule struct {
	PKH   string
	Entry contract.TimetableEntry
}

func Timetable(rows []Schedule) ([]contract.TimetableEntry, error) {
	ordered := append([]Schedule(nil), rows...)
	seen := map[string]bool{}
	for _, row := range ordered {
		if row.PKH == "" || seen[row.PKH] {
			return nil, ErrRecords
		}
		seen[row.PKH] = true
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].PKH < ordered[j].PKH })
	result := make([]contract.TimetableEntry, len(ordered))
	for i, row := range ordered {
		result[i] = row.Entry
	}
	return result, nil
}
