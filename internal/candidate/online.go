package candidate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// Online invokes the production services against the exclusively held candidate.
// Cases are derived from candidate source rows, not fabricated success fixtures.
// No cache, derived-table writer, or publisher is available in this component.
type Online struct {
	Source    read.Matcher
	Grades    grades.Service
	Timetable timetable.Service
	Clock     ports.Clock
	Limits    read.Limits
	MaxCases  int
	memo      *caseMemo
}

func (o Online) Check(ctx context.Context, h read.Handle, op contract.Operation, at time.Time) (result int, summary string, err error) {
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "business_check", Combination: h.Combination, Slot: h.Position.Slot, Generation: h.Generation, Operation: op}, string(h.Position.Instance))
	defer func() { end(err) }()
	if o.Source == nil || o.Clock == nil || o.Grades.Source == nil || o.Grades.Clock == nil || o.Timetable.Source == nil || o.Timetable.Clock == nil || o.MaxCases < 1 || at.IsZero() {
		return 0, "", ErrBuild
	}
	budget, err := read.NewBudget(o.Limits, o.Clock)
	if err != nil {
		return 0, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, o.Limits.Duration)
	defer cancel()
	count := 0
	advance := activity.Begin(ctx, "business_check", "", string(h.Position.Instance), "cases", string(op), -1)
	hash := sha256.New()
	seen := map[string]bool{}
	seenBytes := 0
	visit := func(request any) error {
		if err := budget.Check(ctx); err != nil {
			return err
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			return err
		}
		identity := string(encoded)
		if seen[identity] {
			return nil
		}
		if count >= o.MaxCases {
			return read.ErrBudget
		}
		if len(identity) > 16<<20-seenBytes {
			return read.ErrBudget
		}
		seenBytes += len(identity)
		seen[identity] = true
		parsed, err := transport.Parse(op, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		var response any
		switch req := parsed.(type) {
		case contract.StudentGradesRequest:
			response, err = o.Grades.Student(ctx, h, req, at)
		case contract.BatchGradesRequest:
			response, err = o.Grades.Batch(ctx, h, req, at)
		case contract.StudentTimetableRequest:
			response, err = o.Timetable.Student(ctx, h, req, at)
		case contract.StaffTimetableRequest:
			response, err = o.Timetable.Staff(ctx, h, req, at)
		default:
			return ErrBuild
		}
		if err != nil {
			return err
		}
		payload, err := metadata.Payload(response)
		if err != nil {
			return err
		}
		if len(payload) > 16<<20 {
			return read.ErrBudget
		}
		hash.Write(encoded)
		hash.Write([]byte{'\n'})
		hash.Write(payload)
		hash.Write([]byte{'\n'})
		count++
		advance(1, 0)
		buildmetric.Add(ctx, buildmetric.Cases, 1)
		return budget.Check(ctx)
	}
	if err = o.cases(ctx, h, op, budget, visit); err != nil {
		return 0, "", err
	}
	if count == 0 {
		// Empty sources still exercise a real empty response in every operation.
		const absent = "__readiness_empty__"
		switch op {
		case contract.StudentGrades:
			all := "all"
			err = visit(contract.StudentGradesRequest{StudentNumber: absent, AcademicYear: &all})
		case contract.BatchGrades:
			err = visit(contract.BatchGradesRequest{AcademicYear: "all", StudentNumbers: []string{absent}})
		case contract.StudentTimetable:
			err = visit(contract.StudentTimetableRequest{StudentNumber: absent, AcademicYear: "2000-2001", Semester: "1"})
		case contract.StaffTimetable:
			err = visit(contract.StaffTimetableRequest{StaffNumber: absent, AcademicYear: "2000-2001", Semester: "1"})
		default:
			err = ErrBuild
		}
	}
	if err != nil {
		return 0, "", err
	}
	if err = budget.Check(ctx); err != nil {
		return 0, "", err
	}
	return count, hex.EncodeToString(hash.Sum(nil)), nil
}

func (o Online) cases(ctx context.Context, h read.Handle, op contract.Operation, b *read.Budget, visit func(any) error) error {
	seenTerms := map[[3]string]bool{}
	termBytes := 0
	termVisit := func(id, year, semester string, request any) error {
		key := [3]string{id, year, semester}
		if seenTerms[key] {
			return nil
		}
		charge := len(id) + len(year) + len(semester) + 64
		if len(seenTerms) >= o.MaxCases || charge > 8<<20-termBytes {
			return read.ErrBudget
		}
		termBytes += charge
		seenTerms[key] = true
		return visit(request)
	}
	scan := func(table string, filters []source.Filter, fn func(source.SourceRow) error) error {
		return read.Scan(ctx, o.Source, h, table, filters, b, fn)
	}
	switch op {
	case contract.StudentGrades, contract.BatchGrades:
		var ids []string
		if o.memo != nil && o.memo.gradesLoaded {
			ids = o.memo.gradeIDs
		} else {
			seen := map[string]bool{}
			idBytes := 0
			if err := scan(grades.GradesTable, nil, func(row source.SourceRow) error {
				id, err := compute.Required(row, "XH")
				if err != nil {
					return err
				}
				if !seen[id] {
					if len(ids) >= o.MaxCases || len(id)+64 > 8<<20-idBytes {
						return read.ErrBudget
					}
					idBytes += len(id) + 64
					seen[id] = true
					ids = append(ids, id)
				}
				return nil
			}); err != nil {
				return err
			}
			if o.memo != nil {
				o.memo.gradeIDs = ids
				o.memo.gradesLoaded = true
			}
		}
		for _, id := range ids {
			all := "all"
			page, size := 1, 1
			if op == contract.StudentGrades {
				if err := visit(contract.StudentGradesRequest{StudentNumber: id, AcademicYear: &all, GradeFilters: contract.GradeFilters{Page: &page, PageSize: &size}}); err != nil {
					return err
				}
			} else {
				if err := visit(contract.BatchGradesRequest{AcademicYear: all, StudentNumbers: []string{id}, GradeFilters: contract.GradeFilters{Page: &page, PageSize: &size}}); err != nil {
					return err
				}
			}
		}
		return nil
	case contract.StudentTimetable:
		associations := map[[2]string][][2]string{}
		associationBytes := 0
		return scan(timetable.RelationsTable, nil, func(row source.SourceRow) error {
			id, err := compute.Required(row, "XH")
			if err != nil {
				return err
			}
			class, err := compute.Required(row, "JXBH")
			if err != nil {
				return err
			}
			course, err := compute.Required(row, "JHKCH")
			if err != nil {
				return err
			}
			key := [2]string{class, course}
			if terms, ok := associations[key]; ok {
				for _, term := range terms {
					if err := termVisit(id, term[0], term[1], contract.StudentTimetableRequest{StudentNumber: id, AcademicYear: term[0], Semester: term[1]}); err != nil {
						return err
					}
				}
				return nil
			}
			terms := [][2]string{}
			termSet := map[[2]string]bool{}
			charge := len(class) + len(course) + 64
			cacheable := charge <= 8<<20-associationBytes
			err = scan(timetable.SchedulesTable, []source.Filter{{Column: "JXBH", Value: class}, {Column: "JHKCH", Value: course}}, func(schedule source.SourceRow) error {
				year, err := compute.Required(schedule, "KKXND")
				if err != nil {
					return err
				}
				semester, err := compute.Required(schedule, "KKXQM")
				if err != nil {
					return err
				}
				term := [2]string{year, semester}
				if cacheable && !termSet[term] {
					cost := len(year) + len(semester) + 64
					if cost > 8<<20-associationBytes-charge {
						cacheable = false
						terms = nil
						termSet = nil
					} else {
						charge += cost
						terms = append(terms, term)
						termSet[term] = true
					}
				}
				return termVisit(id, year, semester, contract.StudentTimetableRequest{StudentNumber: id, AcademicYear: year, Semester: semester})
			})
			if err == nil && cacheable {
				associations[key] = terms
				associationBytes += charge
			}
			return err
		})
	case contract.StaffTimetable:
		return scan(timetable.SchedulesTable, nil, func(row source.SourceRow) error {
			year, err := compute.Required(row, "KKXND")
			if err != nil {
				return err
			}
			semester, err := compute.Required(row, "KKXQM")
			if err != nil {
				return err
			}
			teachers, err := timetable.ParseTeachers(row)
			if err != nil {
				return err
			}
			for _, id := range teachers.StaffNumbers {
				if err = termVisit(id, year, semester, contract.StaffTimetableRequest{StaffNumber: id, AcademicYear: year, Semester: semester}); err != nil {
					return err
				}
			}
			return nil
		})
	default:
		return ErrBuild
	}
}
