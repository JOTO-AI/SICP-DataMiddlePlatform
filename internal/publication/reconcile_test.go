package publication_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/publication"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type repairStore struct {
	event    control.Publication
	recorded int
	fail     bool
}

func (s *repairStore) AuthorizePublication(context.Context, control.Publication) (control.Publication, error) {
	return s.event, nil
}
func (s *repairStore) RecordPublicationApplied(_ context.Context, _ control.Publication, at time.Time) error {
	if s.fail {
		return errors.New("control commit failed")
	}
	if s.event.AppliedAt == nil {
		s.event.AppliedAt = &at
		s.recorded++
	}
	return nil
}
func TestT14_08RecoveryAppliesOnlyUnappliedExpectedMetadata(t *testing.T) {
	for _, state := range []string{"expected", "target", "missing", "newer", "ack_failed"} {
		t.Run(state, func(t *testing.T) {
			e, old := event(t)
			db := &repairStore{event: e}
			redis := &atomicMemory{raw: string(*e.Expected)}
			switch state {
			case "target":
				redis.raw = string(e.Target)
			case "missing":
				redis.raw = ""
			case "newer":
				old.Sequence += 10
				raw, _ := control.ActiveDocument(old)
				redis.raw = string(raw)
			case "ack_failed":
				db.fail = true
				redis.raw = string(e.Target)
			}
			atomic := publication.Atomic{Client: redis}
			r := publication.Reconciler{Store: db, Active: atomic, Atomic: atomic, Clock: testkit.FakeClock{Time: e.CreatedAt.Add(time.Minute)}}
			err := r.Apply(t.Context(), e)
			if state == "expected" || state == "target" {
				if err != nil || db.recorded != 1 {
					t.Fatal(err)
				}
			} else if err == nil || db.recorded != 0 {
				t.Fatal("unknown location acknowledged", err)
			}
			writes := 0
			if state == "expected" {
				writes = 1
			}
			if redis.writes != writes {
				t.Fatal("recovery overwrote unexpected metadata")
			}
		})
	}
}
