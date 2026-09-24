package control_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type retirementStub struct{}

func (retirementStub) RetireCollection(_ context.Context, p control.CollectionPlan) (control.Resource, error) {
	r := p.Resource
	r.Retired = true
	return r, nil
}
func TestT14_17CleanupFailureIsIndependentOfPublishedSuccess(t *testing.T) {
	for _, failure := range []string{"cleanup", "record", "none"} {
		t.Run(failure, func(t *testing.T) {
			r, d, p, i := progressFixture(t)
			e := appliedEvent(t, r, p, i)
			at := p.StartedAt.Add(30 * time.Minute)
			confirmed, err := r.ConfirmPublication(t.Context(), e, at)
			if err != nil {
				t.Fatal(err)
			}
			if err = r.CompletePublished(t.Context(), confirmed, at); err != nil {
				t.Fatal(err)
			}
			before := d.task
			published := *d.published
			beforeProgress := len(d.progress)
			files := &retiredFiles{fail: failure == "cleanup"}
			if failure == "record" {
				d.base.failCommit = true
			}
			maintenance := update.Maintenance{Collector: update.Collector{Store: retirementStub{}, Files: files}, Store: r, Clock: testkit.FakeClock{Time: at.Add(time.Minute)}}
			plan := control.CollectionPlan{Resource: control.Resource{ID: "old-files", Kind: "seal", BundleID: "C100"}}
			result := maintenance.Run(t.Context(), plan)
			if result.Cleaned != (failure != "cleanup") || result.Recorded != (failure != "record") {
				t.Fatal(result)
			}
			if !reflect.DeepEqual(d.task, before) || *d.published != published || len(d.progress) != beforeProgress || d.task.Status != "success_published" {
				t.Fatal("maintenance rolled back successful publication")
			}
			recorded := false
			for _, call := range d.base.calls {
				if strings.HasPrefix(call.sql, "INSERT INTO control.steps") {
					recorded = true
					if call.args[0] != "task-A" || call.args[1] != "maintenance/old-files" {
						t.Fatal(call)
					}
				}
			}
			if !recorded {
				t.Fatal("no independent maintenance record")
			}
		})
	}
}
func TestT14_17MaintenanceCannotCompleteUnfinishedTask(t *testing.T) {
	r, d, p, _ := progressFixture(t)
	if err := r.RecordMaintenance(t.Context(), "old-files", false, p.StartedAt.Add(time.Hour)); err == nil || d.task.Status != "running" || d.task.FinishedAt != nil {
		t.Fatal("maintenance rewrote unfinished task", err)
	}
}
