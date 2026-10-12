package teaching

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type Key struct{ AcademicYear, Semester, Class, Course string }
type Group struct {
	Key            Key
	Name           *string
	TeacherNumbers []string
	Schedules      []source.SourceRow
}

func validID(value string) bool {
	if len(value) > 512 || strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 || r == utf8.RuneError {
			return false
		}
	}
	return true
}

// Identity skips incomplete combinations; present invalid values fail. Display
// calendar fields and source-reported enrollment counts are deliberately unused.
func Identity(row source.SourceRow) (Key, bool, error) {
	var key Key
	class, err := compute.Optional(row, "JXBH")
	if err != nil {
		return key, false, err
	}
	course, err := compute.Optional(row, "JHKCH")
	if err != nil {
		return key, false, err
	}
	if class != "" && !validID(class) || course != "" && !validID(course) {
		return key, false, compute.ErrValue
	}
	year, err := compute.Optional(row, "KKXND")
	if err != nil {
		return key, false, err
	}
	semester, err := compute.Optional(row, "KKXQM")
	if err != nil {
		return key, false, err
	}
	if year != "" && !timetable.ValidTerm(year, "1") || semester != "" && semester != "1" && semester != "2" {
		return key, false, compute.ErrValue
	}
	if class == "" || course == "" || year == "" || semester == "" {
		return key, false, nil
	}
	return Key{year, semester, class, course}, true, nil
}

func Aggregate(rows []source.SourceRow, check func() error) ([]Group, error) {
	groups := map[Key]*Group{}
	teachers := map[Key]map[string]bool{}
	seen := map[string]bool{}
	for _, row := range rows {
		if check != nil {
			if err := check(); err != nil {
				return nil, err
			}
		}
		key, present, err := Identity(row)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		id, err := compute.Required(row, "PKH")
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, compute.ErrRecords
		}
		seen[id] = true
		name, err := compute.NullableText(row, "JXBMC")
		if err != nil {
			return nil, err
		}
		ids, err := timetable.ParseTeacherIDs(row)
		if err != nil {
			return nil, err
		}
		group := groups[key]
		if group == nil {
			group = &Group{Key: key}
			groups[key] = group
			teachers[key] = map[string]bool{}
		}
		if name != nil {
			if group.Name != nil && *group.Name != *name {
				return nil, compute.ErrRecords
			}
			group.Name = name
		}
		for _, id := range ids {
			teachers[key][id] = true
		}
		group.Schedules = append(group.Schedules, row)
	}
	result := make([]Group, 0, len(groups))
	for key, group := range groups {
		if check != nil {
			if err := check(); err != nil {
				return nil, err
			}
		}
		group.TeacherNumbers = []string{}
		for id := range teachers[key] {
			group.TeacherNumbers = append(group.TeacherNumbers, id)
		}
		sort.Strings(group.TeacherNumbers)
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i].Key, result[j].Key
		if a.AcademicYear != b.AcademicYear {
			return a.AcademicYear < b.AcademicYear
		}
		if a.Semester != b.Semester {
			return a.Semester < b.Semester
		}
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		return a.Course < b.Course
	})
	return result, nil
}

// ForTeacher selects candidate combinations by complete token, then aggregates
// ALL lessons in those combinations, including lessons by different teachers.
func ForTeacher(rows []source.SourceRow, req contract.StaffTeachingClassesRequest, check func() error) ([]Group, error) {
	if !validID(req.StaffNumber) || !timetable.ValidTerm(req.AcademicYear, req.Semester) || req.CourseCode != nil && !validID(*req.CourseCode) {
		return nil, compute.ErrValue
	}
	candidates := map[Key]bool{}
	keys := make([]Key, len(rows))
	present := make([]bool, len(rows))
	for i, row := range rows {
		if check != nil {
			if err := check(); err != nil {
				return nil, err
			}
		}
		key, ok, err := Identity(row)
		if err != nil {
			return nil, err
		}
		if !ok || key.AcademicYear != req.AcademicYear || key.Semester != req.Semester || req.CourseCode != nil && key.Course != *req.CourseCode {
			continue
		}
		keys[i], present[i] = key, true
		ids, err := timetable.ParseTeacherIDs(row)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if id == req.StaffNumber {
				candidates[key] = true
				break
			}
		}
	}
	selected := []source.SourceRow{}
	for i, row := range rows {
		if check != nil {
			if err := check(); err != nil {
				return nil, err
			}
		}
		if present[i] && candidates[keys[i]] {
			selected = append(selected, row)
		}
	}
	return Aggregate(selected, check)
}
