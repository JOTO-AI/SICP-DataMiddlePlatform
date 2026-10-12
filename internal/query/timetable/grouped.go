package timetable

import (
	"context"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

const StaffGroupSize = 64
const StaffGroupBytes int64 = 32 << 20

// ErrGroupCapacity is returned before any response is emitted. The caller may
// then evaluate the same requests sequentially without discarding cases.
var ErrGroupCapacity = errors.New("staff group retained memory exhausted")

// StaffGroup shares term scans and teacher parsing, then uses the production
// assembler and filters for every request, in original order. It is synchronous
// and only accepts background streaming budgets; foreground admission is intact.
func (s Service) StaffGroup(ctx context.Context, h read.Handle, requests []contract.StaffTimetableRequest, synced time.Time, emit func(int, contract.StaffTimetableResponse) error) error {
	if len(requests) < 1 || len(requests) > StaffGroupSize || emit == nil || s.Limits.BatchBytes == 0 {
		return read.ErrBudget
	}
	type term [2]string
	groups := map[term]map[string][]int{}
	var ordered []term
	for i, req := range requests {
		if _, err := StaffMatches(nil, req); err != nil {
			return err
		}
		key := term{req.AcademicYear, req.Semester}
		if groups[key] == nil {
			groups[key] = map[string][]int{}
			ordered = append(ordered, key)
		}
		groups[key][req.StaffNumber] = append(groups[key][req.StaffNumber], i)
	}
	b, err := read.NewBudget(s.Limits, s.Clock)
	if err != nil {
		return err
	}
	matches := make([][]Match, len(requests))
	var retained int64
	defer func() { buildmetric.Memory(ctx, -retained) }()
	for _, key := range ordered {
		err := read.Scan(ctx, s.Source, h, SchedulesTable, []source.Filter{{Column: "KKXND", Value: key[0]}, {Column: "KKXQM", Value: key[1]}}, b, func(row source.SourceRow) error {
			teachers, err := staffTeachers(row, key[0], key[1])
			if err != nil {
				return err
			}
			var destinations []int
			for _, id := range teachers.StaffNumbers {
				destinations = append(destinations, groups[key][id]...)
			}
			if len(destinations) == 0 {
				return nil
			}
			charge := int64(128 + 64*len(destinations))
			for col, raw := range row {
				charge += int64(len(col) + len(raw) + 64)
			}
			if charge > StaffGroupBytes-retained {
				return ErrGroupCapacity
			}
			retained += charge
			buildmetric.Memory(ctx, charge)
			for _, i := range destinations {
				matches[i] = append(matches[i], Match{Schedule: row})
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for i, req := range requests {
		if err := b.Check(ctx); err != nil {
			return err
		}
		requestBudget, err := read.NewBudget(s.Limits, s.Clock)
		if err != nil {
			return err
		}
		for _, match := range matches[i] {
			if err := requestBudget.Retain(match.Schedule); err != nil {
				return err
			}
		}
		response, err := s.staffResponse(ctx, h, req, synced, matches[i], requestBudget)
		if err != nil {
			return err
		}
		if err := emit(i, response); err != nil {
			return err
		}
	}
	return b.Check(ctx)
}
