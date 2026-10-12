package snapshot_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func phase2Inputs(t *testing.T) map[contract.Operation]map[string]any {
	t.Helper()
	data, err := os.ReadFile("../contract/testdata/phase2.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]map[string]any
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	return map[contract.Operation]map[string]any{
		contract.Organizations: fixtures["organizations_request"], contract.Students: fixtures["students_request"], contract.Staff: fixtures["staff_request"], contract.StaffTeachingClasses: fixtures["staff_teaching_classes_request"], contract.TeachingClassStudents: fixtures["teaching_class_students_request"],
	}
}

func phase2Request(t *testing.T, op contract.Operation, fields map[string]any) any {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	value, err := transport.Parse(op, strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(string(data), err)
	}
	return value
}

func TestT17_04FiveInterfaceContinuationsKeepCombinationAndExpiry(t *testing.T) {
	for op, fields := range phase2Inputs(t) {
		t.Run(string(op), func(t *testing.T) {
			m, s, r, clock := fixture()
			fields["page"] = 7
			input := phase2Request(t, op, fields)
			original, _ := json.Marshal(input)
			first, err := m.Select(t.Context(), op, input, "")
			if err != nil {
				t.Fatal(err)
			}
			fields["page"] = 2
			for field, value := range fields {
				if items, ok := value.([]any); ok {
					reordered := make([]any, 0, len(items)+1)
					for i := len(items) - 1; i >= 0; i-- {
						reordered = append(reordered, items[i])
					}
					fields[field] = append(reordered, items[0])
				}
			}
			moved := first.Handle
			moved.Position = storage.Position{Instance: storage.Standby, Slot: 1}
			moved.Generation++
			r.available["C1"] = moved
			r.active.Combination = "C2"
			clock.Time = clock.Time.Add(time.Minute)
			next, err := m.Select(t.Context(), op, phase2Request(t, op, fields), first.Token)
			if err != nil || next.Handle != moved || next.Token != first.Token || next.ExpiresAt != first.ExpiresAt || r.activeCalls != 1 || len(s.values) != 1 {
				t.Fatal(next, err)
			}
			again, _ := json.Marshal(input)
			if string(again) != string(original) {
				t.Fatal("snapshot binding mutated original request")
			}
			delete(r.available, "C1")
			if _, err := m.Select(t.Context(), op, phase2Request(t, op, fields), first.Token); !errors.Is(err, snapshot.ErrSnapshot) {
				t.Fatal("unreadable bundle accepted", err)
			}
		})
	}
}

func TestT17_05AllEffectiveConditionsAndOperationAreBound(t *testing.T) {
	for op, fields := range phase2Inputs(t) {
		t.Run(string(op), func(t *testing.T) {
			m, _, _, clock := fixture()
			first, err := m.Select(t.Context(), op, phase2Request(t, op, fields), "")
			if err != nil {
				t.Fatal(err)
			}
			for field, original := range fields {
				if field == "page" {
					continue
				}
				var changed any
				switch value := original.(type) {
				case string:
					changed = value + "x"
				case float64:
					changed = value - 1
					if value <= 1 {
						changed = value + 1
					}
				case bool:
					changed = !value
				case []any:
					changed = []any{"different"}
				default:
					t.Fatalf("unhandled fixture field %s", field)
				}
				switch field {
				case "academic_year":
					changed = "2027-2028"
				case "semester":
					changed = "2"
				case "admission_year", "expected_graduation_year":
					changed = "2025"
				}
				fields[field] = changed
				if _, err := m.Select(t.Context(), op, phase2Request(t, op, fields), first.Token); !errors.Is(err, snapshot.ErrSnapshot) {
					t.Fatal("changed condition accepted", field, err)
				}
				fields[field] = original
			}
			for other, otherFields := range phase2Inputs(t) {
				if other == op {
					continue
				}
				if _, err := m.Select(t.Context(), other, phase2Request(t, other, otherFields), first.Token); !errors.Is(err, snapshot.ErrSnapshot) {
					t.Fatal("cross-operation token accepted", other, err)
				}
			}
			for _, token := range []string{"bad", first.Token[:63] + "3"} {
				if _, err := m.Select(t.Context(), op, phase2Request(t, op, fields), token); !errors.Is(err, snapshot.ErrSnapshot) {
					t.Fatal("bad token accepted", err)
				}
			}
			clock.Time = first.ExpiresAt
			if _, err := m.Select(t.Context(), op, phase2Request(t, op, fields), first.Token); !errors.Is(err, snapshot.ErrSnapshot) {
				t.Fatal("expired token accepted", err)
			}
		})
	}
}
