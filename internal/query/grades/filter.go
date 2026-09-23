package grades

import (
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

func MatchCourse(g contract.Grade, f contract.GradeFilters) bool {
	c := g.Course
	if !exact(&c.CourseCode, f.CourseCode) || !contains(c.CourseName, f.CourseName) || !member(c.AttributeCode, f.CourseAttributeCodes) || !member(c.NatureCode, f.CourseNatureCodes) || !member(c.TypeCode, f.CourseTypeCodes) {
		return false
	}
	var code, name *string
	if c.Department != nil {
		code = &c.Department.OrganizationCode
		name = c.Department.OrganizationName
	}
	return exact(code, f.CourseDepartmentCode) && contains(name, f.CourseDepartmentName)
}

func exact(value, filter *string) bool { return filter == nil || value != nil && *value == *filter }
func contains(value, filter *string) bool {
	return filter == nil || value != nil && strings.Contains(*value, *filter)
}
func member(value *string, filters []string) bool {
	if filters == nil {
		return true
	}
	if value == nil {
		return false
	}
	for _, f := range filters {
		if *value == f {
			return true
		}
	}
	return false
}
