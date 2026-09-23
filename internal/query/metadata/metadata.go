// Package metadata composes confirmed synchronization facts outside cached data.
package metadata

import (
	"context"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
)

var ErrMetadata = errors.New("confirmed bundle synchronization metadata unavailable")

type Store interface {
	BundleChecks(context.Context, string) (map[string]time.Time, error)
}

type Provider struct{ Store Store }

// Dependencies are fixed per interface even when a particular result is empty.
// The returned slice belongs to the caller.
func Dependencies(op contract.Operation) ([]string, error) {
	switch op {
	case contract.StudentGrades, contract.BatchGrades:
		return []string{grades.GradesTable, grades.StudentsTable, grades.EnrollmentsTable, grades.CoursesTable, grades.DepartmentsTable, grades.ClassesTable}, nil
	case contract.StudentTimetable:
		return []string{timetable.SchedulesTable, timetable.RelationsTable, timetable.CoursesTable}, nil
	case contract.StaffTimetable:
		return []string{timetable.SchedulesTable, timetable.CoursesTable}, nil
	default:
		return nil, ErrMetadata
	}
}

func (p Provider) SyncedAt(ctx context.Context, h read.Handle, op contract.Operation) (time.Time, error) {
	if p.Store == nil || h.Validate() != nil {
		return time.Time{}, ErrMetadata
	}
	tables, err := Dependencies(op)
	if err != nil {
		return time.Time{}, err
	}
	checks, err := p.Store.BundleChecks(ctx, h.Combination)
	if err != nil {
		return time.Time{}, err
	}
	var minimum time.Time
	for _, table := range tables {
		at, ok := checks[table]
		if !ok || at.IsZero() {
			return time.Time{}, ErrMetadata
		}
		if minimum.IsZero() || at.Before(minimum) {
			minimum = at
		}
	}
	if err = ctx.Err(); err != nil {
		return time.Time{}, err
	}
	return minimum, nil
}
