package control_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type deliveryRedis struct {
	value         string
	calls, writes int
	lose          bool
	after         func()
}

func (r *deliveryRedis) Do(_ context.Context, command string, args ...any) (any, error) {
	r.calls++
	if command == "GET" {
		if r.value == "" {
			return nil, nil
		}
		return r.value, nil
	}
	if command != "EVAL" {
		return nil, errors.New("unexpected non-atomic delivery")
	}
	result := "already_applied"
	target := args[4].(string)
	if r.value != target {
		if r.value != args[3].(string) {
			return "conflict", nil
		}
		r.value = target
		r.writes++
		result = "applied"
	}
	if r.after != nil {
		r.after()
	}
	if r.lose {
		return nil, context.DeadlineExceeded
	}
	return result, nil
}
func deliverer(r control.Repository, m *deliveryRedis, at time.Time) publication.Deliverer {
	return publication.Deliverer{Store: r, Atomic: publication.Atomic{Client: m}, Clock: testkit.FakeClock{Time: at}}
}

func TestT12_08ActualPendingOutboxSurvivesUnknownRedisAndAcknowledgementFailure(t *testing.T) {
	for _, kind := range []string{"reply-lost", "ack-commit"} {
		t.Run(kind, func(t *testing.T) {
			r, d, i := publicationFixture(t)
			e, _, err := r.EnqueuePublication(t.Context(), i)
			if err != nil {
				t.Fatal(err)
			}
			redis := &deliveryRedis{lose: kind == "reply-lost"}
			if kind == "ack-commit" {
				redis.after = func() { d.failCommit = true }
			}
			sender := deliverer(r, redis, i.CreatedAt.Add(time.Minute))
			if err = sender.Apply(t.Context(), e); err == nil {
				t.Fatal("unknown application confirmed")
			}
			stored := d.events[e.ID]
			if stored.Status != "pending" || stored.AppliedAt != nil || stored.ConfirmedAt != nil || redis.writes != 1 {
				t.Fatal(stored, redis)
			}
			d.failCommit = false
			redis.after = nil
			redis.lose = false
			if err = sender.Apply(t.Context(), e); err != nil {
				t.Fatal(err)
			}
			stored = d.events[e.ID]
			if stored.Status != "pending" || stored.AppliedAt == nil || !stored.AppliedAt.Equal(i.CreatedAt.Add(time.Minute)) || stored.ConfirmedAt != nil || redis.writes != 1 {
				t.Fatal(stored, redis)
			}
			for _, call := range d.calls {
				if strings.Contains(call.sql, "source_progress") || strings.Contains(call.sql, "SET published_at") {
					t.Fatal("delivery advanced sync success", call)
				}
			}
		})
	}
}
func TestT12_09ChangedPhysicalGenerationOrForgedEventCannotBeDelivered(t *testing.T) {
	for _, kind := range []string{"generation", "event"} {
		t.Run(kind, func(t *testing.T) {
			r, d, i := publicationFixture(t)
			e, _, err := r.EnqueuePublication(t.Context(), i)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "generation" {
				d.slot.Generation++
			} else {
				target, _ := control.DecodeActive(e.Target)
				target.Position.Generation++
				e.Target, _ = control.ActiveDocument(target)
			}
			redis := &deliveryRedis{}
			if err = deliverer(r, redis, i.CreatedAt.Add(time.Minute)).Apply(t.Context(), e); err == nil || redis.calls != 0 || d.events[i.ID].AppliedAt != nil {
				t.Fatal(err, redis, d.events)
			}
		})
	}
}
