package control_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func acquisitionProgress() control.AcquisitionProgress {
	p := control.AcquisitionProgress{UpdatedAt: time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)}
	for _, table := range source.Catalog() {
		p.Tables = append(p.Tables, control.TableAcquisitionProgress{Table: table.Name, Status: "waiting"})
	}
	total := int64(1440)
	p.Tables[0].Status, p.Tables[0].CompletedPages, p.Tables[0].TotalPages = "acquiring", 720, &total
	return p
}

func TestAcquisitionProgressPersistsOnlyUnderLiveOwnership(t *testing.T) {
	for _, failure := range []string{"none", "owner", "commit", "terminal", "invalid"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			launch, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			db := &checkpointDB{taskDB: d}
			r.Database = db
			if out, err := r.AcquisitionProgress(t.Context(), req.ID); err != nil || out != nil {
				t.Fatal(out, err)
			}
			p := acquisitionProgress()
			switch failure {
			case "owner":
				launch.Owner.Generation++
			case "commit":
				d.failCommit = true
			case "terminal":
				d.records[req.IdempotencyKey][3] = "failed"
			case "invalid":
				p.Tables[0].CompletedPages = 1441
			}
			err = r.WithOwnership(launch.Owner).SaveAcquisitionProgress(t.Context(), p)
			if failure != "none" {
				if err == nil || db.document != "" {
					t.Fatal("invalid progress persisted", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.AcquisitionProgress(t.Context(), req.ID)
			if err != nil || !reflect.DeepEqual(out, &p) {
				t.Fatal(out, err)
			}
			// Terminal tasks retain the last observation without claiming success.
			d.records[req.IdempotencyKey][3] = "failed"
			out, err = r.AcquisitionProgress(t.Context(), req.ID)
			if err != nil || !reflect.DeepEqual(out, &p) {
				t.Fatal(out, err)
			}
			db.document = `{"tables":[],"updated_at":"2026-09-30T04:00:00Z","secret":"private"}`
			if out, err = r.AcquisitionProgress(t.Context(), req.ID); err == nil || out != nil {
				t.Fatal("invalid stored progress returned")
			}
		})
	}
}

func TestRestorationProgressRoundTripsBeforeTableCompletion(t *testing.T) {
	for _, status := range []string{"checking", "reusing"} {
		for _, complete := range []int64{0, 720, 1440} {
			r, d, req := taskRepository(t)
			launch, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			r.Database = &checkpointDB{taskDB: d}
			p := acquisitionProgress()
			p.Tables[0].Status, p.Tables[0].CompletedPages = status, complete
			if complete == 0 {
				p.Tables[0].TotalPages = nil
			}
			if err := r.WithOwnership(launch.Owner).SaveAcquisitionProgress(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			got, err := r.AcquisitionProgress(t.Context(), req.ID)
			if err != nil || !reflect.DeepEqual(got, &p) {
				t.Fatal(got, err)
			}
		}
	}
}
