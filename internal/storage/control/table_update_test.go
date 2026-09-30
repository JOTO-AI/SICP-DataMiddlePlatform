package control_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

func TestReusedInitializationWatermarksRequirePersistedEpoch(t *testing.T) {
	r, d, p, _ := progressFixture(t)
	d.epoch = p.StartedAt.Add(-2 * time.Hour)
	p.Retained = true
	for table := range p.Ends {
		p.Ends[table] = d.epoch
	}
	if err := r.StageProgress(t.Context(), p); err != nil {
		t.Fatal("valid retained source epoch rejected", err)
	}
	r, d, p, _ = progressFixture(t)
	d.epoch = p.StartedAt.Add(-2 * time.Hour)
	p.Retained = true
	if err := r.StageProgress(t.Context(), p); !errors.Is(err, control.ErrEntity) {
		t.Fatal("reused tables advanced to retry time", err)
	}
}

func TestSingleTableProgressCannotAdvanceOtherTables(t *testing.T) {
	r, d, p, _ := progressFixture(t)
	_, planDB, active := planFixture(t)
	planRepo := control.Repository{Registry: r.Registry, Database: planDB}.WithOwnership(control.Ownership{TaskID: "task-A", OwnerID: "owner-A", Generation: 9})
	planDB.table = "dwd_bzkscjzlb"
	planDB.task.Mode = "force_full"
	plan, err := planRepo.PlanUpdate(t.Context(), active, 0)
	if err != nil {
		t.Fatal(err)
	}
	p.StartedAt = plan.StartedAt
	p.CheckedAt = p.StartedAt.Add(time.Minute)
	p.Mode = "force_full"
	p.Retained = true
	d.task.Mode = p.Mode
	d.task.StartedAt = &p.StartedAt
	raw, _ := json.Marshal(plan)
	roles := control.Document(raw)
	d.task.Roles = &roles
	for name := range p.Ends {
		p.Ends[name] = plan.Windows[name].ConfirmedUntil
	}
	p.Ends[plan.Table] = p.StartedAt
	if err := r.StageProgress(t.Context(), p); err != nil {
		t.Fatal("single-table bounds rejected", err)
	}
	p.Ends["dwd_xqjbsjzlb"] = p.StartedAt
	if err := r.StageProgress(t.Context(), p); !errors.Is(err, control.ErrEntity) {
		t.Fatal("unfetched table watermark advanced", err)
	}
}

func TestTableUpdateCanRepairFailedInitializationAndPersistsIdempotentScope(t *testing.T) {
	r, d, old := taskRepository(t)
	old.Mode = "initialize"
	if _, err := r.Admit(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	d.records[old.IdempotencyKey][3] = "failed"
	req := control.Request{ID: "repair", IdempotencyKey: "repair-key", RequestedBy: "admin-1", Table: "dwd_bzkscjzlb", AcceptedAt: old.AcceptedAt.Add(time.Minute)}
	got, err := r.AdmitTable(t.Context(), req)
	if err != nil || got.Task.Mode != "initialize" || got.Task.PredecessorID == nil || *got.Task.PredecessorID != old.ID {
		t.Fatal("initialization repair rejected", err)
	}
	if selected, err := r.TableSelection(t.Context(), got.Task.ID); err != nil || selected != req.Table {
		t.Fatal(selected, err)
	}
	// Replay survives later initialization/publication and returns the old task.
	d.initialized = true
	again, err := r.AdmitTable(t.Context(), req)
	if err != nil || again.Created || again.Task.ID != got.Task.ID {
		t.Fatal("non-idempotent replay", err)
	}
	req.Table = "dwd_xqjbsjzlb"
	if _, err := r.AdmitTable(t.Context(), req); !errors.Is(err, control.ErrIdempotency) {
		t.Fatal("same key changed table", err)
	}
	if _, err := r.Admit(t.Context(), control.Request{ID: "full", IdempotencyKey: req.IdempotencyKey, RequestedBy: req.RequestedBy, AcceptedAt: req.AcceptedAt, Mode: "initialize"}); !errors.Is(err, control.ErrIdempotency) {
		t.Fatal("table request became whole update", err)
	}
}

func TestTableUpdateSharesExecutionGateAndRollsBackScope(t *testing.T) {
	for _, scenario := range []string{"published", "busy", "invalid", "rollback"} {
		t.Run(scenario, func(t *testing.T) {
			r, d, req := taskRepository(t)
			d.initialized = true
			req.Table = "dwd_bzkscjzlb"
			if scenario == "busy" {
				d.owner = control.Ownership{TaskID: "other", OwnerID: "worker", Generation: 1}
			}
			if scenario == "invalid" {
				req.Table = "arbitrary_table"
			}
			if scenario == "rollback" {
				d.failCommit = true
			}
			got, err := r.AdmitTable(t.Context(), req)
			if scenario == "published" {
				if err != nil || got.Task.Mode != "force_full" {
					t.Fatal(err)
				}
			} else if err == nil || len(d.records) != 0 || len(d.selections) != 0 {
				t.Fatal("invalid admission mutated state", err)
			}
		})
	}
}

func TestRerunRepairsAllMissingInitializationTablesButKeepsPublishedTableScope(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		r, d, req := taskRepository(t)
		d.initialized = initialized
		req.Table = "dwd_bzkscjzlb"
		old, err := r.AdmitTable(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		d.records[req.IdempotencyKey][3] = "failed"
		rerun, err := r.RerunTask(t.Context(), control.Request{ID: "rerun-table", IdempotencyKey: "rerun-table-key", RequestedBy: req.RequestedBy, AcceptedAt: req.AcceptedAt.Add(time.Minute), PredecessorID: &old.Task.ID})
		if err != nil {
			t.Fatal(err)
		}
		selected, err := r.TableSelection(t.Context(), rerun.Task.ID)
		want := ""
		if initialized {
			want = req.Table
		}
		if err != nil || selected != want {
			t.Fatal("wrong rerun scope", selected, err)
		}
	}
}
