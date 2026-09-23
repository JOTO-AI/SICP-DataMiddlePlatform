package grades

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// Student selects the term first, assembles whole rows, then intersects all
// filters. No pagination is applied until this complete result is available.
func (a Assembler) Student(rows []source.SourceRow, req contract.StudentGradesRequest) ([]contract.Grade, error) {
	scoped, err := StudentScope(rows, req)
	if err != nil {
		return nil, err
	}
	result := []contract.Grade{}
	for _, row := range scoped {
		grade, err := a.Grade(row)
		if err != nil {
			return nil, err
		}
		if MatchCourse(grade, req.GradeFilters) && MatchScore(grade, req.GradeFilters) {
			result = append(result, grade)
		}
	}
	return result, nil
}
