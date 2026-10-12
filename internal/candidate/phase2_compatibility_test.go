package candidate_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
)

func TestT24_04HistoricalEvidenceUsesFrozenFourOperations(t *testing.T) {
	_, target, manifest, _, _, _ := buildFixture(t)
	legacyOps := []contract.Operation{contract.StudentGrades, contract.BatchGrades, contract.StudentTimetable, contract.StaffTimetable}
	definitions := contract.LegacyBusinessDefinitions()
	actual := []contract.Operation{}
	for _, definition := range definitions {
		actual = append(actual, definition.Operation)
	}
	if !reflect.DeepEqual(actual, legacyOps) {
		t.Fatal(actual)
	}
	definitions[0].Operation = contract.Organizations
	if contract.LegacyBusinessDefinitions()[0].Operation != contract.StudentGrades {
		t.Fatal("mutable legacy catalog")
	}
	makeEvidence := func() candidate.Evidence {
		ev := candidate.Evidence{Target: target, Combination: manifest.Combination, CheckedAt: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), Queries: map[contract.Operation]candidate.QueryEvidence{}}
		for _, op := range legacyOps {
			names, err := metadata.Dependencies(op)
			if err != nil {
				t.Fatal(err)
			}
			deps := map[string]string{}
			for _, name := range names {
				for _, table := range manifest.Tables {
					if name == table.Table {
						deps[name] = table.Scope + ":" + table.SHA256
					}
				}
			}
			ev.Queries[op] = candidate.QueryEvidence{Policy: "bounded-smoke-v2", Dependencies: deps, Cases: 1, SHA256: strings.Repeat("a", 64)}
		}
		return ev
	}
	if err := makeEvidence().Validate(target, manifest); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing old", "replace with new", "extra new", "all nine", "storage label", "unknown policy"} {
		t.Run(kind, func(t *testing.T) {
			ev := makeEvidence()
			switch kind {
			case "missing old":
				delete(ev.Queries, contract.StudentGrades)
			case "replace with new":
				ev.Queries[contract.Organizations] = ev.Queries[contract.StudentGrades]
				delete(ev.Queries, contract.StudentGrades)
			case "extra new":
				ev.Queries[contract.Organizations] = ev.Queries[contract.StudentGrades]
			case "all nine":
				for _, op := range []contract.Operation{contract.Organizations, contract.Students, contract.Staff, contract.StaffTeachingClasses, contract.TeachingClassStudents} {
					ev.Queries[op] = ev.Queries[contract.StudentGrades]
				}
			case "storage label":
				ev.Policy = candidate.StorageCheckPolicy
			case "unknown policy":
				ev.Policy = "nine-business-v1"
			}
			if err := ev.Validate(target, manifest); err == nil {
				t.Fatal("legacy evidence broadened", kind)
			}
		})
	}
	fresh, err := validator(&validations{}).Validate(t.Context(), target, manifest)
	if err != nil || fresh.Policy != candidate.StorageCheckPolicy || len(fresh.Queries) != 0 || len(fresh.Tables) != len(manifest.Tables) {
		t.Fatal(fresh, err)
	}
}
