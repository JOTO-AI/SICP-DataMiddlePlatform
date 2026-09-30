package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type repairHistory struct {
	tasks    map[string]control.Task
	selected map[string]string
}

func (h repairHistory) TaskStatus(_ context.Context, id string) (control.TaskStatus, error) {
	return control.TaskStatus{Task: h.tasks[id]}, nil
}
func (h repairHistory) TableSelection(_ context.Context, id string) (string, error) {
	return h.selected[id], nil
}
func (h repairHistory) SourceEpoch(context.Context, string) (*time.Time, error) { return nil, nil }

func TestInterruptedRepairRetainsAncestorTablesAndConservativeEpoch(t *testing.T) {
	dir, err := staging.OpenDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	files, err := staging.NewQuotaFiles(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := staging.NewStore("first", files, 1<<20)
	limits := ingest.PageLimits{MaxPages: 100, MaxRecords: 1000}
	table := "dwd_xqjbsjzlb"
	request := ingest.PageRequest{Table: table, Mode: ingest.Full, Page: 1, PageSize: 2}
	pager, _ := ingest.NewPager(table, 2, limits)
	page := ingest.Page{Total: 0}
	if err := pager.Accept(1, page); err != nil {
		t.Fatal(err)
	}
	if err := first.CommitPage(t.Context(), request, page, pager.Manifest().Pages[0]); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)
	later := start.Add(time.Hour)
	firstID, secondID := "first", "second"
	history := repairHistory{tasks: map[string]control.Task{
		firstID:  {ID: firstID, Mode: "initialize", Status: "failed", StartedAt: &start},
		secondID: {ID: secondID, Mode: "initialize", Status: "failed", StartedAt: &later, PredecessorID: &firstID},
	}, selected: map[string]string{secondID: "dwd_bzkscjzlb"}}
	// Second task crashed before copying this table or persisting an epoch.
	task := control.Task{ID: "third", StartedAt: &later, PredecessorID: &secondID}
	ancestors, epoch, err := initializationAncestors(t.Context(), task, history, files)
	if err != nil || !epoch.Equal(start) {
		t.Fatal("lost original source epoch", err, epoch)
	}
	target, _ := staging.NewStore(task.ID, files, 1<<20)
	if pages, err := ancestors.copyTable(t.Context(), request, target, limits, nil); err != nil || pages != 1 {
		t.Fatal("interruption lost completed ancestor table", pages, err)
	}
	// An interrupted explicit refresh must not silently revive its old version.
	history.selected[secondID] = table
	ancestors, _, err = initializationAncestors(t.Context(), task, history, files)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := staging.NewStore("fourth", files, 1<<20)
	if _, err := ancestors.copyTable(t.Context(), request, other, limits, nil); !errors.Is(err, staging.ErrIncomplete) {
		t.Fatal("revived invalidated table", err)
	}
	if entries, err := other.Entries(t.Context()); err != nil || len(entries) != 0 {
		t.Fatal("partial copy", err)
	}
	history.tasks[firstID] = control.Task{ID: firstID, Mode: "initialize", Status: "failed", StartedAt: &start, PredecessorID: &secondID}
	if _, _, err := initializationAncestors(t.Context(), task, history, files); !errors.Is(err, control.ErrEntity) {
		t.Fatal("lineage cycle accepted", err)
	}
}
