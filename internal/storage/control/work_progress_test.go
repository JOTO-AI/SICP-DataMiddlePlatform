package control_test

import (
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
)

func TestWorkProgressIsFencedAndRetainedAfterCompletion(t *testing.T) {
	for _, failure := range []string{"none", "owner", "terminal", "invalid", "commit", "published_pending_progress"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			launch, err := r.Dispatch(t.Context(), req, "worker", req.AcceptedAt, nil)
			if err != nil {
				t.Fatal(err)
			}
			db := &checkpointDB{taskDB: d}
			r.Database = db
			p := activity.Snapshot{Phase: "building_table", Table: "dwd_bzkscjzlb", StartedAt: req.AcceptedAt, HeartbeatAt: req.AcceptedAt}
			switch failure {
			case "owner":
				launch.Owner.Generation++
			case "terminal":
				d.records[req.IdempotencyKey][3] = "failed"
			case "published_pending_progress":
				d.records[req.IdempotencyKey][3] = "published_pending_progress"
			case "invalid":
				p.Table = "private-source-payload"
			case "commit":
				d.failCommit = true
			}
			err = r.WithOwnership(launch.Owner).SaveWorkProgress(t.Context(), p)
			if failure != "none" && failure != "published_pending_progress" {
				if err == nil || db.document != "" {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			d.records[req.IdempotencyKey][3] = "success_published"
			got, err := r.WorkProgress(t.Context(), req.ID)
			if err != nil || !reflect.DeepEqual(got, &p) {
				t.Fatal(got, err)
			}
			if err = r.WithOwnership(launch.Owner).SaveWorkProgress(t.Context(), p); err == nil {
				t.Fatal("finished task kept heartbeating")
			}
		})
	}
}
