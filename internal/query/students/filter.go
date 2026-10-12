package students

import (
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

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
func year(value, filter *string) bool {
	return filter == nil || value != nil && strings.HasPrefix(*value, *filter+"-")
}

func Match(item contract.Student, req contract.StudentsRequest) bool {
	var departmentCode, departmentName, counselor *string
	if item.Department != nil {
		departmentCode = &item.Department.OrganizationCode
		departmentName = item.Department.OrganizationName
	}
	if item.Counselor != nil {
		counselor = &item.Counselor.StaffNumber
	}
	for _, p := range [][2]*string{
		{&item.StudentNumber, req.StudentNumber}, {departmentCode, req.DepartmentCode}, {item.CampusCode, req.CampusCode},
		{item.MajorCode, req.MajorCode}, {item.Grade, req.Grade}, {item.ClassCode, req.ClassCode}, {item.OnCampusFlag, req.OnCampusFlag}, {counselor, req.CounselorStaffNumber},
	} {
		if !exact(p[0], p[1]) {
			return false
		}
	}
	if !contains(&item.Name, req.Name) || !contains(departmentName, req.DepartmentName) {
		return false
	}
	if !member(item.StatusCode, req.StatusCodes) || !member(item.EnrollmentStatusCode, req.EnrollmentStatusCodes) || !member(item.StudentCategoryCode, req.StudentCategoryCodes) {
		return false
	}
	if req.StudyYears != nil && (item.StudyYears == nil || *item.StudyYears != *req.StudyYears) {
		return false
	}
	return year(item.AdmissionMonth, req.AdmissionYear) && year(item.ExpectedGraduationDate, req.ExpectedGraduationYear)
}
