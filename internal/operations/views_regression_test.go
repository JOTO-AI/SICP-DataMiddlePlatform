package operations

import (
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"strings"
	"testing"
)

func TestT31_04And05SourceFailureIsBoundedControlFact(t *testing.T) {
	for _, tc := range []struct{ name, checked, want string }{
		{"unresolved", "null", "unresolved"},
		{"old_success", `"2026-10-12T00:00:00Z"`, "unresolved"},
		{"recovered", `"2026-10-14T00:00:00Z"`, "superseded_by_success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := map[string]int{}
			tx := &fakeTx{row: func(q string, args []any) storage.Row {
				return fakeRow(func(d ...any) error {
					if strings.Contains(q, "roles::text") {
						*d[0].(**string) = nil
						return nil
					}
					if len(args) != 1 || !strings.Contains(q, "t.status='failed'") || !strings.Contains(q, "item->>'table'=$1") {
						return errors.New("unbound failure lookup")
					}
					raw := `{"checked_at":` + tc.checked + `,"task_id":"success-task","last_failure":{"task_id":"failure-task","code":"acquisition_failed","observed_at":"2026-10-13T00:00:00Z"}}`
					*d[0].(**string) = &raw
					return nil
				})
			}}
			items, err := fixtureRepo(tx).Sources(t.Context(), func(id string) string { calls[id]++; return "correlated-" + id })
			if err != nil || len(items) != 14 {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.FailureState != tc.want || item.LastFailure == nil || item.LastFailure.LogTaskID != "correlated-failure-task" {
					t.Fatal("failure state changed", item)
				}
				if item.ConfirmedUntil != nil {
					t.Fatal("failure invented successful progress")
				}
			}
			if calls["success-task"] != 14 || calls["failure-task"] != 14 || len(calls) != 2 {
				t.Fatal("identity was rehashed")
			}
		})
	}
}

func TestT31_05BrokenFailureNeverBecomesEmptySuccess(t *testing.T) {
	tx := &fakeTx{row: func(q string, args []any) storage.Row {
		return fakeRow(func(d ...any) error {
			if strings.Contains(q, "roles::text") {
				*d[0].(**string) = nil
				return nil
			}
			raw := `{"last_failure":{"task_id":"failed-task","code":"raw-secret","observed_at":"2026-10-13T00:00:00Z"}}`
			*d[0].(**string) = &raw
			return nil
		})
	}}
	if items, err := fixtureRepo(tx).Sources(t.Context(), func(v string) string { return v }); err != ErrUnavailable || items != nil || tx.committed {
		t.Fatal("corrupt failure returned partial data", err)
	}
}
