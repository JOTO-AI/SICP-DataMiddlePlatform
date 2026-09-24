//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/application"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

// Reconstruct every worker dependency with the same persistent files/databases
// and a new execution identity. OS/container restart is additionally tested in I06.
func restartWorker(t *testing.T, f *runtimeFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	previous := f.wr.ProcessID
	if err := f.worker.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.wr.Close(ctx); err != nil {
		t.Fatal(err)
	}
	factories := assembly.RealFactories()
	factories.Redis = func(context.Context, config.Runtime) (assembly.Redis, error) { return f.redis, nil }
	runtime, _, err := assembly.Open(f.e.ctx, f.wc, factories)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	app, err := application.New(f.wc, runtime)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	if runtime.ProcessID == previous {
		t.Fatal("worker execution identity reused on restart")
	}
	f.worker, f.wr = app, runtime
}
func sourceRequests(f *runtimeFixture) int {
	f.source.mu.Lock()
	defer f.source.mu.Unlock()
	return f.source.requests
}
func TestI05_01(t *testing.T) {
	for _, fault := range []string{"lost_redis_ack", "failed_control_confirmation"} {
		t.Run(fault, func(t *testing.T) {
			f := newRuntime(t, true)
			var writes atomic.Int64
			f.redis.afterDo = func(command string, args []any, value any, err error) (any, error) {
				if command == "EVAL" && strings.Contains(args[4].(string), `"event_id":"next-publish"`) && err == nil {
					writes.Add(1)
					if fault == "lost_redis_ack" {
						return nil, errors.New("synthetic lost publication acknowledgement")
					}
				}
				return value, err
			}
			removeFault := func() {}
			if fault == "failed_control_confirmation" {
				conn := f.e.connection(t, 4)
				var once sync.Once
				removeFault = func() {
					once.Do(func() {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if _, err := conn.Exec(ctx, "DROP TRIGGER IF EXISTS integration_confirmation ON control.publication_outbox; DROP FUNCTION IF EXISTS control.integration_confirmation()"); err != nil {
							t.Error(err)
						}
					})
				}
				t.Cleanup(removeFault)
				if _, err := conn.Exec(f.e.ctx, `CREATE FUNCTION control.integration_confirmation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='next-publish' AND NEW.status='confirmed' THEN RAISE EXCEPTION 'synthetic confirmation failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER integration_confirmation BEFORE UPDATE ON control.publication_outbox FOR EACH ROW EXECUTE FUNCTION control.integration_confirmation()`); err != nil {
					t.Fatal(err)
				}
			}
			admitTask(t, f, "force_full", "next")
			if err := f.worker.Engine.Cycle(f.e.ctx); err == nil {
				t.Fatal("publication fault not observed")
			}
			before := activeMetadata(t, f)
			if before.Position.BundleID != "Cnext" || writes.Load() != 1 || countControl(t, f, "SELECT count(*) FROM control.publication_outbox WHERE id='next-publish' AND status='pending'") != 1 {
				t.Fatal("fault did not occur after real Redis application")
			}
			requests := sourceRequests(f)
			removeFault()
			restartWorker(t, f)
			if err := f.worker.Engine.Cycle(f.e.ctx); err != nil {
				t.Fatal("publication recovery", err)
			}
			completed := loadTask(t, f, "next")
			if completed.Status != "success_published" || completed.PublishedAt == nil || activeMetadata(t, f) != before || writes.Load() != 1 || sourceRequests(f) != requests {
				t.Fatal("recovery republished/reacquired instead of confirming")
			}
			if countControl(t, f, "SELECT count(*) FROM control.publication_outbox WHERE id='next-publish' AND status='confirmed'") != 1 {
				t.Fatal("publication not confirmed exactly once")
			}
			assertProgress(t, f, "next")
			if err := f.worker.Engine.Cycle(f.e.ctx); err != nil {
				t.Fatal(err)
			}
			again := loadTask(t, f, "next")
			if !again.PublishedAt.Equal(*completed.PublishedAt) || writes.Load() != 1 || sourceRequests(f) != requests {
				t.Fatal("completed recovery was not idempotent")
			}
			f.queryOK(t, contract.StudentGrades, allGrades, "")
		})
	}
}
func TestI05_02(t *testing.T) {
	f := mainBaseline(t)
	f.run(t, "force_full", "101")
	current := activeMetadata(t, f)
	for _, id := range []string{"init-publish", "100-shift", "100-publish"} {
		event := control.Publication{ID: id}
		if err := f.wr.Control.Load(f.e.ctx, &event); err != nil {
			t.Fatal(err)
		}
		event.Status = "pending" // Replay the immutable old delivery envelope.
		if err := f.wr.Active.Apply(f.e.ctx, event); !errors.Is(err, publication.ErrConflict) {
			t.Fatalf("old event %s accepted: %v", id, err)
		}
		if activeMetadata(t, f) != current {
			t.Fatal("old event overwrote newer complete metadata")
		}
	}
}
func TestI05_04(t *testing.T) {
	f := newRuntime(t, true)
	until := realClock{}.Now().Add(time.Minute)
	if err := f.wr.Control.ExtendCooldown(f.e.ctx, until); err != nil {
		t.Fatal(err)
	}
	restartWorker(t, f)
	stored, err := f.wr.Control.Cooldown(f.e.ctx)
	if err != nil || !stored.Equal(until) {
		t.Fatal("cooldown did not survive restart", err)
	}
	before := sourceRequests(f)
	admitTask(t, f, "incremental", "waiting")
	if err := f.worker.Engine.Cycle(f.e.ctx); !errors.Is(err, control.ErrBusy) {
		t.Fatalf("cooldown allowed start: %v", err)
	}
	task := loadTask(t, f, "waiting")
	if task.Status != "accepted" || task.StartedAt != nil || sourceRequests(f) != before {
		t.Fatal("cooldown invoked source early")
	}
	// Expire only this case's persisted clock fixture, avoiding a minute sleep.
	if _, err := f.e.connection(t, 4).Exec(f.e.ctx, "UPDATE control.schedule SET cooldown_until=$1", realClock{}.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := f.worker.Engine.Cycle(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	if task := loadTask(t, f, "waiting"); task.Status != "success_unchanged" || sourceRequests(f) != before+14 {
		t.Fatal("expired cooldown did not allow one acquisition")
	}
}
func TestI05_05(t *testing.T) {
	f := newRuntime(t, true)
	at := realClock{}.Now()
	if _, err := f.e.connection(t, 4).Exec(f.e.ctx, "UPDATE control.schedule SET next_due_at=$1", at); err != nil {
		t.Fatal(err)
	}
	schedule, err := f.wr.Control.CurrentSchedule(f.e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		launch control.Launch
		err    error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, id := range []string{"manual", "timer"} {
		go func(id string) {
			<-start
			var revision *int64
			if id == "timer" {
				value := schedule.Revision
				revision = &value
			}
			launch, err := f.wr.Control.Dispatch(f.e.ctx, control.Request{ID: id, IdempotencyKey: id, Mode: "incremental", RequestedBy: id, AcceptedAt: at.Add(-time.Second)}, f.wr.ProcessID, at, revision)
			results <- result{launch, err}
		}(id)
	}
	close(start)
	var winner control.Launch
	winners := 0
	completed := []result{<-results, <-results}
	for _, out := range completed {
		if out.err != nil && !errors.Is(out.err, control.ErrStaleTimer) {
			t.Fatal(out.err)
		}
		if out.launch.Started {
			winners++
			winner = out.launch
		}
	}
	if winners != 1 || countControl(t, f, "SELECT count(*) FROM control.tasks WHERE id IN ('manual','timer')") != 1 {
		t.Fatal("competition started multiple tasks")
	}
	current, err := f.wr.Control.CurrentSchedule(f.e.ctx)
	if err != nil || current.Revision != schedule.Revision+1 || !current.LastStartedAt.Equal(at) || !current.NextDueAt.Equal(at.Add(control.SyncInterval)) || !winner.Task.StartedAt.Equal(at) {
		t.Fatal("schedule was not rebased on actual start", err)
	}
	before := sourceRequests(f)
	if err := f.worker.Engine.Work.Run(f.e.ctx, winner.Task, winner.Owner, false, nil); err != nil {
		t.Fatal(err)
	}
	if sourceRequests(f) != before+14 || loadTask(t, f, winner.Task.ID).Status != "success_unchanged" {
		t.Fatal("winning task did not execute exactly one round")
	}
}
func TestI05_07(t *testing.T) {
	f := mainBaseline(t)
	changeGradeNow(t, f)
	f.run(t, "incremental", "101")
	first := activeMetadata(t, f)
	if first.Position.BundleID != "C101" || first.Position.Position != (storage.Position{Instance: storage.Standby, Slot: 0}) {
		t.Fatal("first update direction incorrect")
	}
	changeGradeNow(t, f)
	f.source.mu.Lock()
	f.source.rows["dwd_bzkscjzlb"][1] = json.RawMessage(strings.Replace(string(f.source.rows["dwd_bzkscjzlb"][1]), `"KCCJ":91`, `"KCCJ":92`, 1))
	f.source.mu.Unlock()
	f.run(t, "incremental", "102")
	second := activeMetadata(t, f)
	if second.Position.BundleID != "C102" || second.Position.Position != (storage.Position{Instance: storage.Main, Slot: 1}) {
		t.Fatal("second update direction incorrect")
	}
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		for slot := 0; slot < 2; slot++ {
			index := slot
			if instance == storage.Standby {
				index += 2
			}
			var generation int64
			var bundle string
			if err := f.e.connection(t, 4).QueryRow(f.e.ctx, "SELECT generation,bundle_id FROM control.slots WHERE instance=$1 AND slot=$2", string(instance), slot).Scan(&generation, &bundle); err != nil || generation != 2 || bundle != fmt.Sprintf("C%d", 101+slot) {
				t.Fatal("four-slot identity mismatch", err)
			}
			var score string
			if err := f.e.connection(t, index).QueryRow(f.e.ctx, `SELECT "KCCJ"::text FROM source.dwd_bzkscjzlb WHERE "ID"='G02'`).Scan(&score); err != nil || score != fmt.Sprint(91+slot) {
				t.Fatal("four-slot content mismatch", err)
			}
		}
	}
	for _, tc := range []struct{ id, first, second string }{{"101", "S", "M"}, {"102", "M", "S"}} {
		task := loadTask(t, f, tc.id)
		var roles struct{ First, Second string }
		if task.Roles == nil || json.Unmarshal([]byte(*task.Roles), &roles) != nil || roles.First != tc.first || roles.Second != tc.second {
			t.Fatal("durable update roles changed")
		}
	}
	assertProgress(t, f, "102")
	data, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
	if data["items"].([]any)[1].(map[string]any)["score"] != float64(92) {
		t.Fatal("query did not use second update")
	}
}
