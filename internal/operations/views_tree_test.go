package operations

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

func TestT31_01RecoveryRetainsEventIdentityAndRerunLinksPreviousTask(t *testing.T) {
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	previous := "failed-task"
	for _, state := range []string{"running", "published_pending_progress", "success_published"} {
		t.Run(state, func(t *testing.T) {
			rows := &viewRows{items: [][]any{
				{"recovered-task", "force_full", state, nil, at, &at, nil, nil, nil, nil},
				{"rerun-task", "force_full", "accepted", &previous, at.Add(-time.Hour), nil, nil, nil, nil, nil},
				{previous, "force_full", "failed", nil, at.Add(-2 * time.Hour), nil, nil, nil, nil, nil},
			}}
			tx := &viewTx{query: func(string, []any) (storage.Rows, error) { return rows, nil }}
			got, err := (Repository{Database: viewDB{tx}}).Events(t.Context(), at, "", 3)
			if err != nil || len(got) != 3 || got[0].ID != "recovered-task" || got[0].TaskID != got[0].ID || got[0].Predecessor != nil || got[0].Status != state {
				t.Fatal("recovery changed identity", got, err)
			}
			if got[1].ID == got[2].ID || got[1].Predecessor == nil || *got[1].Predecessor != got[2].ID || got[2].Status != "failed" {
				t.Fatal("rerun replaced predecessor", got)
			}
			if got[0].PublishedAt != nil || got[0].FinishedAt != nil {
				t.Fatal("status fabricated missing timestamps")
			}
		})
	}
}

func TestT31_02And03TreeUsesPersistedFactsAndStableParentPaging(t *testing.T) {
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	m, s, slot, generation := "M", "S", int16(1), int64(7)
	for _, tc := range []struct {
		parent, evidence, queryFragment string
		rows                            [][]any
	}{
		{"builds", "control", "control.candidates", [][]any{
			{"copy-M", "building", &m, &slot, &generation, nil, nil, "builds"},
			{"copy-S", "ready", &s, &slot, &generation, nil, &at, "builds"},
		}},
		{"publication", "control", "confirmed_at,kind", [][]any{
			{"event-a-shift", "confirmed", nil, nil, nil, &at, &at, "route_shift"},
			{"event-b-publish", "pending", nil, nil, nil, &at, nil, "bundle_publish"},
		}},
		{"acquisition", "control_checkpoint", "c.name='acquisition-progress'", [][]any{
			{"source-a", "collected", nil, nil, nil, nil, nil, "acquisition"},
			{"source-b", "reusing", nil, nil, nil, nil, nil, "acquisition"},
		}},
		{"progress", "control_checkpoint", "c.name='successful-progress-plan'", [][]any{
			{"successful-progress-plan", "staged", nil, nil, nil, nil, nil, "progress"},
		}},
		{"progress", "control_checkpoint", "t.checked_at=(c.payload->>'CheckedAt')::timestamptz", [][]any{
			{"successful-progress-plan", "confirmed", nil, nil, nil, nil, &at, "progress"},
		}},
		{"cleanup", "control", "name LIKE 'maintenance/%'", [][]any{
			{"maintenance/version-a", "failed", nil, nil, nil, nil, &at, "cleanup"},
			{"maintenance/version-b", "succeeded", nil, nil, nil, nil, &at, "cleanup"},
		}},
	} {
		t.Run(tc.parent, func(t *testing.T) {
			after := ""
			for _, record := range tc.rows {
				rows := &viewRows{items: [][]any{record}}
				tx := &viewTx{fakeTx: fakeTx{row: func(q string, a []any) storage.Row {
					if len(a) != 1 || a[0] != "recovered-task" {
						t.Fatal("cross-task existence check")
					}
					return fakeRow(func(d ...any) error { *d[0].(*bool) = true; return nil })
				}}, query: func(q string, a []any) (storage.Rows, error) {
					if !strings.Contains(q, tc.queryFragment) || !reflect.DeepEqual(a, []any{"recovered-task", after, 1}) || strings.Contains(q, "control.source_progress") {
						t.Fatal("wrong fact source, task or cursor", q, a)
					}
					return rows, nil
				}}
				got, err := (Repository{Database: viewDB{tx}}).Operations(t.Context(), "recovered-task", tc.parent, after, 1)
				if err != nil || len(got) != 1 {
					t.Fatal(got, err)
				}
				v := got[0]
				if v.ID != record[0] || v.State != record[1] || v.Kind != record[7] || v.Parent == nil || *v.Parent != tc.parent || v.Evidence != tc.evidence || v.Count != nil || v.Build != nil {
					t.Fatal("altered control fact", v)
				}
				if record[5] == nil && v.StartedAt != nil || record[6] == nil && v.FinishedAt != nil || !rows.closed || !tx.committed {
					t.Fatal("invented boundary or unclosed snapshot", v)
				}
				after = v.ID
			}
		})
	}
}

func TestT31_03OperationPagePreservesDatabaseCollationAndBounds(t *testing.T) {
	rows := &viewRows{items: [][]any{
		{"copy-a", "ready", nil, nil, nil, nil, nil, "builds"},
		{"copy-Z", "ready", nil, nil, nil, nil, nil, "builds"},
	}}
	tx := &viewTx{fakeTx: fakeTx{row: func(string, []any) storage.Row {
		return fakeRow(func(v ...any) error { *v[0].(*bool) = true; return nil })
	}}, query: func(string, []any) (storage.Rows, error) { return rows, nil }}
	r := Repository{Database: viewDB{tx}}
	got, err := r.Operations(t.Context(), "task", "builds", "", 2)
	if err != nil || len(got) != 2 || got[0].ID != "copy-a" || got[1].ID != "copy-Z" {
		t.Fatal("page reordered independently of SQL cursor", got, err)
	}
	for _, after := range []string{strings.Repeat("x", 513), "bad\x00cursor", string([]byte{0xff})} {
		if got, err := (Repository{}).Operations(t.Context(), "task", "builds", after, 1); err != ErrArgument || got != nil {
			t.Fatal("invalid cursor reached storage", err)
		}
	}
	for _, limit := range []int{0, 202} {
		if _, err := (Repository{}).Operations(t.Context(), "task", "builds", "", limit); err != ErrArgument {
			t.Fatal("unbounded operation page", err)
		}
	}
}

type treeFailureDB struct {
	tx  storage.Transaction
	err error
}

func (d treeFailureDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	return d.tx, d.err
}

type treeFailureTx struct {
	viewTx
	commitErr error
}

func (tx *treeFailureTx) Commit(ctx context.Context) error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	return tx.fakeTx.Commit(ctx)
}

func TestT31_03ControlReadFailuresNeverReturnPartialTreeOrHistory(t *testing.T) {
	private := errors.New("private database failure")
	for _, history := range []bool{false, true} {
		for _, failure := range []string{"begin", "snapshot", "existence", "missing", "query", "scan", "rows", "overflow", "commit", "cancel"} {
			if history && (failure == "existence" || failure == "missing") {
				continue
			}
			t.Run(failure+"/history="+map[bool]string{true: "true", false: "false"}[history], func(t *testing.T) {
				at := time.Now().UTC()
				record := []any{"copy", "ready", nil, nil, nil, nil, nil, "builds"}
				if history {
					record = []any{"task", "force_full", "running", nil, at, nil, nil, nil, nil, nil}
				}
				rows := &viewRows{items: [][]any{record}}
				if failure == "scan" {
					rows.items = append(rows.items, []any{"invalid-column-count"})
				}
				if failure == "overflow" {
					rows.items = append(rows.items, record)
				}
				if failure == "rows" {
					rows.err = private
				}
				tx := &treeFailureTx{viewTx: viewTx{fakeTx: fakeTx{
					exec: func(string, []any) (int64, error) {
						if failure == "snapshot" {
							return 0, private
						}
						return 1, nil
					},
					row: func(string, []any) storage.Row {
						return fakeRow(func(d ...any) error {
							if failure == "existence" {
								return private
							}
							*d[0].(*bool) = failure != "missing"
							return nil
						})
					},
				}, query: func(string, []any) (storage.Rows, error) {
					if failure == "query" {
						return nil, private
					}
					return rows, nil
				}}}
				db := treeFailureDB{tx: tx}
				if failure == "begin" {
					db.err = private
				}
				if failure == "commit" {
					tx.commitErr = private
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if failure == "cancel" {
					cancel()
				}
				r := Repository{Database: db}
				var err error
				if history {
					var got []Event
					got, err = r.Events(ctx, at, "", 1)
					if got != nil {
						t.Fatal("partial history")
					}
				} else {
					var got []Operation
					got, err = r.Operations(ctx, "task", "builds", "", 1)
					if got != nil {
						t.Fatal("partial tree")
					}
				}
				want := ErrUnavailable
				if failure == "missing" {
					want = ErrMissing
				}
				if err != want || tx.committed || failure != "begin" && !tx.rolled {
					t.Fatal("failure disguised as success", err)
				}
			})
		}
	}
}
