package grades

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// MatchStudent treats the requested identifiers as a set: the caller visits
// each source grade once, so repeated IDs never multiply records.
func MatchStudent(g contract.Grade, req contract.BatchGradesRequest) bool {
	s := g.Student
	return member(&s.StudentNumber, req.StudentNumbers) && exact(s.DepartmentCode, req.StudentDepartmentCode) && contains(s.DepartmentName, req.StudentDepartmentName) && exact(s.ClassCode, req.ClassCode) && contains(s.ClassName, req.ClassName)
}

// BatchScope applies one explicit term range to all students. It never invokes
// StudentScope's latest-term policy, even when a student has no recent grades.
func BatchScope(rows []source.SourceRow, req contract.BatchGradesRequest) ([]source.SourceRow, error) {
	if req.AcademicYear != "all" {
		if _, err := yearRank(req.AcademicYear); err != nil {
			return nil, err
		}
	}
	if req.Semester != nil && *req.Semester != "1" && *req.Semester != "2" {
		return nil, compute.ErrValue
	}
	result := []source.SourceRow{}
	for _, row := range rows {
		year, err := compute.Required(row, "XN")
		if err != nil {
			return nil, err
		}
		if _, err := yearRank(year); err != nil {
			return nil, err
		}
		semester, err := compute.Required(row, "XQM")
		if err != nil || semester != "1" && semester != "2" {
			return nil, compute.ErrValue
		}
		if req.AcademicYear != "all" && year != req.AcademicYear {
			continue
		}
		if req.Semester != nil && semester != *req.Semester {
			continue
		}
		result = append(result, row)
	}
	return result, nil
}
