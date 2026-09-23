package grades

import (
	"strconv"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// StudentScope selects the term before course/score filters and counts only
// this student's source records. It must never see already score-filtered data.
func StudentScope(rows []source.SourceRow, req contract.StudentGradesRequest) ([]source.SourceRow, error) {
	if strings.TrimSpace(req.StudentNumber) == "" {
		return nil, compute.ErrValue
	}
	if req.Semester != nil && *req.Semester != "1" && *req.Semester != "2" {
		return nil, compute.ErrValue
	}
	if req.AcademicYear != nil && *req.AcademicYear != "all" {
		if _, err := yearRank(*req.AcademicYear); err != nil {
			return nil, err
		}
	}
	type record struct {
		row            source.SourceRow
		year, semester string
		rank           int
	}
	candidates := []record{}
	latest := -1
	for _, row := range rows {
		student, err := compute.Required(row, "XH")
		if err != nil {
			return nil, err
		}
		if student != req.StudentNumber {
			continue
		}
		year, err := compute.Required(row, "XN")
		if err != nil {
			return nil, err
		}
		rank, err := yearRank(year)
		if err != nil {
			return nil, err
		}
		semester, err := compute.Required(row, "XQM")
		if err != nil || semester != "1" && semester != "2" {
			return nil, compute.ErrValue
		}
		if req.AcademicYear != nil && *req.AcademicYear != "all" && year != *req.AcademicYear {
			continue
		}
		if req.Semester != nil && semester != *req.Semester {
			continue
		}
		rank = rank*2 + int(semester[0]-'0')
		if rank > latest {
			latest = rank
		}
		candidates = append(candidates, record{row, year, semester, rank})
	}
	result := []source.SourceRow{}
	for _, candidate := range candidates {
		// With only semester specified, all candidates share the same semester;
		// the largest term rank therefore identifies its most recent year.
		if req.AcademicYear == nil && candidate.rank != latest {
			continue
		}
		result = append(result, candidate.row)
	}
	return result, nil
}

func yearRank(year string) (int, error) {
	if len(year) != 9 || year[4] != '-' {
		return 0, compute.ErrValue
	}
	for i, c := range year {
		if i != 4 && (c < '0' || c > '9') {
			return 0, compute.ErrValue
		}
	}
	a, err := strconv.Atoi(year[:4])
	if err != nil {
		return 0, compute.ErrValue
	}
	b, _ := strconv.Atoi(year[5:])
	if a < 1000 || b != a+1 {
		return 0, compute.ErrValue
	}
	return a, nil
}
