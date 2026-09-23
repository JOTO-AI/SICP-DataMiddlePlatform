package control_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type recoverableLogs struct {
	down bool
	ids  map[string]bool
}

func (s *recoverableLogs) StoreBatch(_ context.Context, batch []observability.SafeEvent) error {
	if s.down {
		return errors.New("logs offline")
	}
	if s.ids == nil {
		s.ids = map[string]bool{}
	}
	for _, e := range batch {
		s.ids[e.ID()] = true
	}
	return nil
}
func TestT05_18LogDeliveryFailureDoesNotRollbackCommittedControlFact(t *testing.T) {
	ctx := context.Background()
	r, db, req := taskRepository(t)
	accepted, err := r.Accept(ctx, req)
	if err != nil || !accepted.Created {
		t.Fatal("control fact not committed")
	}
	beforeRollbacks := db.rollbacks
	beforeCalls := len(db.calls)
	s, err := observability.NewSanitizer(4096, 256, []byte(strings.Repeat("k", 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := observability.OpenDirectory(t.TempDir(), testkit.FixedRandom(9), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	q, err := observability.NewQuota(files, observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 1000, WarningPercent: 80, Policy: observability.RejectNew})
	if err != nil {
		t.Fatal(err)
	}
	b, err := observability.NewBuffer(q, s, 10, 32768)
	if err != nil {
		t.Fatal(err)
	}
	guard, _ := observability.NewGuard(s, b)
	e := events.Event{ID: "accepted-event", EntityID: accepted.Task.ID, Kind: events.Task, Phase: events.Started, Identity: events.Identity{TaskID: accepted.Task.ID}, OccurredAt: accepted.Task.AcceptedAt}
	if err := guard.Record(ctx, e); err != nil {
		t.Fatal(err)
	}
	logs := &recoverableLogs{down: true}
	if _, err := b.Deliver(ctx, logs); err != observability.ErrDelivery {
		t.Fatal("expected delivery failure")
	}
	if db.rollbacks != beforeRollbacks || len(db.calls) != beforeCalls {
		t.Fatal("logging mutated or rolled back control transaction")
	}
	loaded := control.Task{ID: req.ID}
	if err := r.Load(ctx, &loaded); err != nil || loaded.Status != "accepted" || !loaded.AcceptedAt.Equal(req.AcceptedAt) {
		t.Fatal("committed task fact lost")
	}
	rebuilt, err := observability.NewBuffer(q, s, 10, 32768)
	if err != nil {
		t.Fatal(err)
	}
	logs.down = false
	if n, err := rebuilt.Deliver(ctx, logs); err != nil || n != 1 || len(logs.ids) != 1 {
		t.Fatal("control event not recoverable after logs recovery")
	}
}
