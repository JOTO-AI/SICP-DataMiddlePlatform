package timetable

import (
	"errors"
	"strconv"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrAmbiguous = errors.New("ambiguous student membership term")

type relationKey struct{ class, course string }
type term struct{ year, semester string }
type Match struct {
	Schedule   source.SourceRow
	Membership []source.SourceRow
}

func validTerm(year, semester string) bool {
	if len(year) != 9 || year[4] != '-' || semester != "1" && semester != "2" {
		return false
	}
	for i, ch := range year {
		if i != 4 && (ch < '0' || ch > '9') {
			return false
		}
	}
	a, _ := strconv.Atoi(year[:4])
	b, _ := strconv.Atoi(year[5:])
	return a >= 1000 && b == a+1
}

// StudentMatches requires ALL terms for the relevant relation keys: the source
// membership table has no term, so prefiltering schedules would hide ambiguity.
func StudentMatches(relations, schedules []source.SourceRow, req contract.StudentTimetableRequest) ([]Match, error) {
	if strings.TrimSpace(req.StudentNumber) == "" || !validTerm(req.AcademicYear, req.Semester) {
		return nil, compute.ErrValue
	}
	members := map[relationKey][]source.SourceRow{}
	for _, r := range relations {
		student, err := compute.Required(r, "XH")
		if err != nil {
			return nil, err
		}
		if student != req.StudentNumber {
			continue
		}
		class, err := compute.Required(r, "JXBH")
		if err != nil {
			return nil, err
		}
		course, err := compute.Required(r, "JHKCH")
		if err != nil {
			return nil, err
		}
		key := relationKey{class, course}
		members[key] = append(members[key], r)
	}
	if len(members) == 0 {
		return []Match{}, nil
	}
	type candidate struct {
		row  source.SourceRow
		key  relationKey
		term term
	}
	candidates := []candidate{}
	terms := map[relationKey]map[term]bool{}
	for _, r := range schedules {
		class, err := compute.Required(r, "JXBH")
		if err != nil {
			return nil, err
		}
		course, err := compute.Required(r, "JHKCH")
		if err != nil {
			return nil, err
		}
		key := relationKey{class, course}
		if len(members[key]) == 0 {
			continue
		}
		year, err := compute.Required(r, "KKXND")
		if err != nil {
			return nil, err
		}
		semester, err := compute.Required(r, "KKXQM")
		if err != nil {
			return nil, err
		}
		if !validTerm(year, semester) {
			return nil, compute.ErrValue
		}
		when := term{year, semester}
		if terms[key] == nil {
			terms[key] = map[term]bool{}
		}
		terms[key][when] = true
		candidates = append(candidates, candidate{r, key, when})
	}
	selected := term{req.AcademicYear, req.Semester}
	out := []Match{}
	for _, c := range candidates {
		if c.term != selected {
			continue
		}
		if len(terms[c.key]) != 1 {
			return nil, ErrAmbiguous
		}
		out = append(out, Match{Schedule: c.row, Membership: members[c.key]})
	}
	return out, nil
}
