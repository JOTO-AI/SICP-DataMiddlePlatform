package timetable

import (
	"strings"
	"unicode"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type Teachers struct {
	StaffNumbers []string
	Names        []string
}

// ParseTeachers preserves independently supplied arrays. It never pairs names
// with IDs by index or splits an opaque ID on punctuation or whitespace.
func ParseTeachers(row source.SourceRow) (Teachers, error) {
	ids, err := ParseTeacherIDs(row)
	if err != nil {
		return Teachers{}, err
	}
	names, err := teacherField(row, "JSXM", true)
	if err != nil {
		return Teachers{}, err
	}
	return Teachers{StaffNumbers: ids, Names: names}, nil
}

// ParseTeacherIDs shares the exact existing token rules without requiring
// display names. Class aggregation resolves names from staff profiles instead.
func ParseTeacherIDs(row source.SourceRow) ([]string, error) {
	return teacherField(row, "JSJGH", false)
}

func teacherField(row source.SourceRow, field string, name bool) ([]string, error) {
	value, err := compute.Text(row, field)
	if err != nil {
		return nil, err
	}
	if value == nil || strings.TrimSpace(*value) == "" {
		return []string{}, nil
	}
	items, err := tokens(row, field)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		for _, ch := range item {
			valid := unicode.IsLetter(ch) || unicode.IsNumber(ch) || ch == '-' || ch == '.' || ch == '_'
			if name {
				valid = valid || unicode.IsMark(ch) || strings.ContainsRune(" ·•()（）'", ch)
			}
			if !valid {
				return nil, compute.ErrValue
			}
		}
		if !seen[item] {
			out = append(out, item)
			seen[item] = true
		}
	}
	return out, nil
}

func (t Teachers) Contains(staffNumber string) bool {
	for _, id := range t.StaffNumbers {
		if id == staffNumber {
			return true
		}
	}
	return false
}
