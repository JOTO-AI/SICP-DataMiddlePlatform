package teaching_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/teaching"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

var handle = read.Handle{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 9, Combination: "C9"}
var key = teaching.Key{AcademicYear: "2026-2027", Semester: "1", Class: "J1", Course: "C1"}

type membershipRepo struct {
	tables map[string][]source.SourceRow
	calls  int
	failAt int
	cancel context.CancelFunc
}

func (r *membershipRepo) ReadMatching(ctx context.Context, h read.Handle, table, after string, limit int, filters []source.Filter) ([]source.SourceRow, error) {
	r.calls++
	if h != handle {
		return nil, errors.New("changed handle")
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("missing SQL deadline")
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.calls == r.failAt {
		return nil, errors.New("SQL failed")
	}
	if len(filters) != 1 || filters[0].Column != "JXBH" || filters[0].Values == nil {
		return nil, errors.New("term restriction or unbounded read")
	}
	pk := "PKH"
	if table == timetable.RelationsTable {
		pk = "ID"
	}
	values := map[string]bool{}
	for _, v := range filters[0].Values {
		values[v] = true
	}
	result := []source.SourceRow{}
	for _, row := range r.tables[table] {
		id, _ := compute.Required(row, pk)
		class, _ := compute.Optional(row, "JXBH")
		if id > after && values[class] {
			result = append(result, row)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, _ := compute.Required(result[i], pk)
		b, _ := compute.Required(result[j], pk)
		return a < b
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func membershipFixture() *membershipRepo {
	return &membershipRepo{tables: map[string][]source.SourceRow{timetable.SchedulesTable: {schedule("p1", "J1", "C1", "T1"), schedule("p2", "J1", "C1", "T2")}, timetable.RelationsTable: {row(`{"ID":"r1","JXBH":"J1","JHKCH":"C1","XH":"01","ETL_TIME":"2026-09-18 08:00:00"}`), row(`{"ID":"r2","JXBH":"J1","JHKCH":"C1","XH":"001"}`), row(`{"ID":"r3","JXBH":"J1","JHKCH":"C1","XH":"01","ETL_TIME":"2026-09-18 09:00:00"}`), row(`{"ID":"r4","JXBH":"J1","JHKCH":"other","XH":null}`)}}}
}
func budget(t *testing.T, limits read.Limits) *read.Budget {
	t.Helper()
	b, err := read.NewBudget(limits, testkit.FakeClock{Time: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestT21_04CombinationExistenceAndEmptyRoster(t *testing.T) {
	repo := membershipFixture()
	repo.tables[timetable.RelationsTable] = nil
	got, err := (teaching.Membership{Source: repo}).Resolve(t.Context(), handle, []teaching.Key{key}, budget(t, read.DefaultLimits()))
	if err != nil || len(got) != 1 || len(got[key].Schedules) != 2 || got[key].Members == nil || len(got[key].Members) != 0 || repo.calls != 2 {
		t.Fatal(got, err, repo.calls)
	}
	repo = membershipFixture()
	missing := key
	missing.Semester = "2"
	got, err = (teaching.Membership{Source: repo}).Resolve(t.Context(), handle, []teaching.Key{missing}, budget(t, read.DefaultLimits()))
	if !errors.Is(err, teaching.ErrNotFound) || got != nil || repo.calls != 1 {
		t.Fatal(got, err, repo.calls)
	}
	repo = membershipFixture()
	repo.tables[timetable.SchedulesTable] = nil
	if got, err := (teaching.Membership{Source: repo}).Resolve(t.Context(), handle, []teaching.Key{key}, budget(t, read.DefaultLimits())); !errors.Is(err, teaching.ErrNotFound) || got != nil {
		t.Fatal(got, err)
	}
}

func TestT21_05CrossTermReuseFailsBeforeMembershipRead(t *testing.T) {
	for _, empty := range []bool{false, true} {
		for _, field := range []string{"KKXQM", "KKXND"} {
			repo := membershipFixture()
			old := schedule("p3", "J1", "C1", "DIFFERENT")
			if field == "KKXQM" {
				old[field] = json.RawMessage(`"2"`)
			} else {
				old[field] = json.RawMessage(`"2025-2026"`)
			}
			repo.tables[timetable.SchedulesTable] = append(repo.tables[timetable.SchedulesTable], old)
			if empty {
				repo.tables[timetable.RelationsTable] = nil
			}
			got, err := (teaching.Membership{Source: repo}).Resolve(t.Context(), handle, []teaching.Key{key}, budget(t, read.DefaultLimits()))
			if !errors.Is(err, teaching.ErrTermConflict) || got != nil || repo.calls != 1 {
				t.Fatal(empty, field, got, err, repo.calls)
			}
		}
	}
	// Different course in the same teaching class does not create ambiguity.
	repo := membershipFixture()
	other := schedule("p3", "J1", "other", "T3")
	other["KKXQM"] = json.RawMessage(`"2"`)
	repo.tables[timetable.SchedulesTable] = append(repo.tables[timetable.SchedulesTable], other)
	if _, err := (teaching.Membership{Source: repo}).Resolve(t.Context(), handle, []teaching.Key{key}, budget(t, read.DefaultLimits())); err != nil {
		t.Fatal(err)
	}
}

func TestT21_06UniqueMembersKeepAllProvenanceAndFailures(t *testing.T) {
	repo := membershipFixture()
	got, err := (teaching.Membership{Source: repo}).Resolve(t.Context(), handle, []teaching.Key{key, key}, budget(t, read.DefaultLimits()))
	if err != nil || len(got) != 1 || len(got[key].Members) != 2 || got[key].Members[0].StudentNumber != "001" || got[key].Members[1].StudentNumber != "01" || len(got[key].Members[1].Relations) != 2 || !reflect.DeepEqual(got[key].Members[1].Relations[1], repo.tables[timetable.RelationsTable][2]) {
		t.Fatal(got, err)
	}
	second := key
	second.Class = "J2"
	repo = membershipFixture()
	repo.tables[timetable.SchedulesTable] = append(repo.tables[timetable.SchedulesTable], schedule("p3", "J2", "C1", "T1"))
	repo.tables[timetable.RelationsTable] = append(repo.tables[timetable.RelationsTable], row(`{"ID":"r5","JXBH":"J2","JHKCH":"C1","XH":"different"}`))
	got, err = (teaching.Membership{Source: repo}).Resolve(t.Context(), handle, []teaching.Key{key, second}, budget(t, read.DefaultLimits()))
	if err != nil || len(got) != 2 || len(got[key].Members) != 2 || len(got[second].Members) != 1 || got[second].Members[0].StudentNumber != "different" || repo.calls != 2 {
		t.Fatal(got, err, repo.calls)
	}
	for _, kind := range []string{"missing student", "duplicate relation", "invalid term", "SQL", "late SQL", "budget", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			repo := membershipFixture()
			limits := read.DefaultLimits()
			ctx := t.Context()
			switch kind {
			case "missing student":
				repo.tables[timetable.RelationsTable][0]["XH"] = json.RawMessage(`null`)
			case "duplicate relation":
				repo.tables[timetable.RelationsTable] = append(repo.tables[timetable.RelationsTable], repo.tables[timetable.RelationsTable][0])
			case "invalid term":
				repo.tables[timetable.SchedulesTable][0]["KKXQM"] = json.RawMessage(`"3"`)
			case "SQL":
				repo.failAt = 2
			case "late SQL":
				limits.BatchSize = 2
				repo.failAt = 4
			case "budget":
				limits.Rows = 2
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				repo.cancel = cancel
			}
			got, err := (teaching.Membership{Source: repo}).Resolve(ctx, handle, []teaching.Key{key}, budget(t, limits))
			if err == nil || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}
