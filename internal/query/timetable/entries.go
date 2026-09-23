package timetable

import (
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

func (a Assembler) Entries(matches []Match) ([]contract.TimetableEntry, error) {
	return a.entriesChecked(matches, nil)
}

func (a Assembler) entriesChecked(matches []Match, check func() error) ([]contract.TimetableEntry, error) {
	rows := make([]compute.Schedule, 0, len(matches))
	for _, match := range matches {
		if check != nil {
			if err := check(); err != nil {
				return nil, err
			}
		}
		entry, err := a.Schedule(match.Schedule, match.Membership...)
		if err != nil {
			return nil, err
		}
		rows = append(rows, entry)
	}
	// Identity is PKH. Different rooms/dates/classes for one course remain
	// separate; conflicting source identities must not be silently collapsed.
	return compute.Timetable(rows)
}
