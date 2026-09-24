package control_test

import (
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

func TestT14_08LostPublicationReplyRecoversWithoutAnotherRedisWrite(t *testing.T) {
	r, d, p, i := progressFixture(t)
	if err := r.StageProgress(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	e, _, err := r.EnqueuePublication(t.Context(), i)
	if err != nil {
		t.Fatal(err)
	}
	redis := &deliveryRedis{lose: true}
	at := p.StartedAt.Add(30 * time.Minute)
	if err = deliverer(r, redis, at).Apply(t.Context(), e); err == nil || redis.writes != 1 || d.base.events[e.ID].AppliedAt != nil {
		t.Fatal("not unknown", err)
	}
	redis.lose = false
	before := redis.calls
	clock := testkit.FakeClock{Time: at.Add(time.Minute)}
	atomic := publication.Atomic{Client: redis}
	repair := publication.Reconciler{Store: r, Active: atomic, Atomic: atomic, Clock: clock}
	f := update.Finisher{Store: r, Delivery: repair, Clock: clock}
	confirmed, err := f.Recover(t.Context(), e)
	if err != nil || redis.writes != 1 || redis.calls != before+1 || d.task.Status != "success_published" || len(d.progress) != 14 {
		t.Fatal(confirmed, err, redis)
	}
	published, finished, checked := *d.task.PublishedAt, *d.task.FinishedAt, *d.task.CheckedAt
	f.Clock = testkit.FakeClock{Time: at.Add(time.Hour)}
	before = redis.calls
	if _, err = f.Recover(t.Context(), e); err != nil || redis.calls != before || *d.task.PublishedAt != published || *d.task.FinishedAt != finished || *d.task.CheckedAt != checked {
		t.Fatal("replay rewrote confirmed times", err)
	}
}

func TestT14_08AppliedTimeSurvivesControlConfirmationFailure(t *testing.T) {
	r, d, p, i := progressFixture(t)
	e := appliedEvent(t, r, p, i)
	original := *d.base.events[e.ID].AppliedAt
	redis := &deliveryRedis{value: string(e.Target)}
	atomic := publication.Atomic{Client: redis}
	clock := testkit.FakeClock{Time: original.Add(time.Hour)}
	repair := publication.Reconciler{Store: r, Active: atomic, Atomic: atomic, Clock: clock}
	if err := repair.Apply(t.Context(), e); err != nil || *d.base.events[e.ID].AppliedAt != original || redis.writes != 0 {
		t.Fatal(err)
	}
	d.base.failCommit = true
	if _, err := r.ConfirmPublication(t.Context(), e, clock.Now()); err == nil || d.task.PublishedAt != nil {
		t.Fatal("failed commit escaped", err)
	}
	d.base.failCommit = false
	confirmed, err := r.ConfirmPublication(t.Context(), e, clock.Now())
	if err != nil || confirmed.Status != "confirmed" || *d.task.PublishedAt != original {
		t.Fatal(confirmed, err)
	}
}

func TestT14_09And13RecoveryRejectsMissingActualTargetAndStaleOwner(t *testing.T) {
	for _, failure := range []string{"missing_applied", "wrong_actual", "stale_owner"} {
		t.Run(failure, func(t *testing.T) {
			r, d, p, i := progressFixture(t)
			e := appliedEvent(t, r, p, i)
			redis := &deliveryRedis{value: string(e.Target)}
			switch failure {
			case "missing_applied":
				redis.value = ""
			case "wrong_actual":
				active, _ := control.DecodeActive(e.Target)
				active.Sequence++
				doc, _ := control.ActiveDocument(active)
				redis.value = string(doc)
			case "stale_owner":
				r = r.WithOwnership(control.Ownership{TaskID: "task-A", OwnerID: "old-worker", Generation: 8})
			}
			atomic := publication.Atomic{Client: redis}
			repair := publication.Reconciler{Store: r, Active: atomic, Atomic: atomic, Clock: testkit.FakeClock{Time: i.CreatedAt.Add(time.Hour)}}
			if err := repair.Apply(t.Context(), e); err == nil || redis.writes != 0 || d.task.PublishedAt != nil || len(d.progress) != 0 {
				t.Fatal("unsafe recovery", err)
			}
			if failure == "stale_owner" && redis.calls != 0 {
				t.Fatal("stale owner reached Redis")
			}
		})
	}
}
