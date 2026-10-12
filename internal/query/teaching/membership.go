package teaching

import (
	"context"
	"errors"
	"sort"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrNotFound = errors.New("teaching class combination not found")
var ErrTermConflict = errors.New("teaching class membership has ambiguous term")

type Member struct {
	StudentNumber string
	Relations     []source.SourceRow
}
type Roster struct {
	Schedules []source.SourceRow
	Members   []Member
}
type Membership struct{ Source read.Matcher }
type pairKey struct{ class, course string }

func reserveRows(ctx context.Context, b *read.Budget, rows []source.SourceRow) error {
	for _, row := range rows {
		if err := b.Check(ctx); err != nil {
			return err
		}
		if err := b.Reserve(512); err != nil {
			return err
		}
		for _, raw := range row {
			if err := b.Reserve(len(raw)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Resolve reads ALL terms for the requested class/course pairs before reading
// membership, whose source has no term. Both public APIs share this exact set.
func (m Membership) Resolve(ctx context.Context, h read.Handle, keys []Key, b *read.Budget) (map[Key]Roster, error) {
	if m.Source == nil || h.Validate() != nil {
		return nil, read.ErrHandle
	}
	if err := b.Check(ctx); err != nil {
		return nil, err
	}
	requested := map[Key]bool{}
	ordered := []Key{}
	pairs := map[pairKey]bool{}
	classes := []string{}
	for _, key := range keys {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		if !validID(key.Class) || !validID(key.Course) || !timetable.ValidTerm(key.AcademicYear, key.Semester) {
			return nil, compute.ErrValue
		}
		if requested[key] {
			continue
		}
		if err := b.Reserve(len(key.Class) + len(key.Course) + len(key.AcademicYear) + 256); err != nil {
			return nil, err
		}
		requested[key] = true
		ordered = append(ordered, key)
		pairs[pairKey{key.Class, key.Course}] = true
		classes = append(classes, key.Class)
	}
	result := map[Key]Roster{}
	if len(requested) == 0 {
		return result, nil
	}
	schedules, err := read.CollectKeys(ctx, m.Source, h, timetable.SchedulesTable, "JXBH", classes, b)
	if err != nil {
		return nil, err
	}
	if err := reserveRows(ctx, b, schedules); err != nil {
		return nil, err
	}
	terms := map[pairKey]map[Key][]source.SourceRow{}
	for _, row := range schedules {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		class, err := compute.Optional(row, "JXBH")
		if err != nil {
			return nil, err
		}
		course, err := compute.Optional(row, "JHKCH")
		if err != nil {
			return nil, err
		}
		pair := pairKey{class, course}
		if !pairs[pair] {
			continue
		}
		key, present, err := Identity(row)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		if terms[pair] == nil {
			terms[pair] = map[Key][]source.SourceRow{}
		}
		terms[pair][key] = append(terms[pair][key], row)
	}
	// Establish existence/ambiguity for EVERY candidate, even empty rosters.
	for _, key := range ordered {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		available := terms[pairKey{key.Class, key.Course}]
		if len(available[key]) == 0 {
			return nil, ErrNotFound
		}
		if len(available) != 1 {
			return nil, ErrTermConflict
		}
		result[key] = Roster{Schedules: available[key], Members: []Member{}}
	}
	relations, err := read.CollectKeys(ctx, m.Source, h, timetable.RelationsTable, "JXBH", classes, b)
	if err != nil {
		return nil, err
	}
	if err := reserveRows(ctx, b, relations); err != nil {
		return nil, err
	}
	members := map[pairKey]map[string][]source.SourceRow{}
	for _, row := range relations {
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		class, err := compute.Optional(row, "JXBH")
		if err != nil {
			return nil, err
		}
		course, err := compute.Optional(row, "JHKCH")
		if err != nil {
			return nil, err
		}
		pair := pairKey{class, course}
		if !pairs[pair] {
			continue
		}
		id, err := compute.Required(row, "XH")
		if err != nil {
			return nil, err
		}
		if !validID(id) {
			return nil, compute.ErrValue
		}
		if members[pair] == nil {
			members[pair] = map[string][]source.SourceRow{}
		}
		members[pair][id] = append(members[pair][id], row)
	}
	for _, key := range ordered {
		roster := result[key]
		if err := b.Check(ctx); err != nil {
			return nil, err
		}
		for id, rows := range members[pairKey{key.Class, key.Course}] {
			if err := b.Check(ctx); err != nil {
				return nil, err
			}
			roster.Members = append(roster.Members, Member{StudentNumber: id, Relations: rows})
		}
		sort.Slice(roster.Members, func(i, j int) bool { return roster.Members[i].StudentNumber < roster.Members[j].StudentNumber })
		result[key] = roster
	}
	if err := b.Check(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
