package update_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type shiftWorld struct {
	trace  []string
	fail   string
	busy   bool
	event  control.Publication
	intent control.PublicationIntent
}

func (w *shiftWorld) step(s string) error {
	w.trace = append(w.trace, s)
	if w.fail == s {
		return errors.New("synthetic shift failure")
	}
	return nil
}
func (w *shiftWorld) AuthorizeShift(context.Context, update.ReadabilityProof) error {
	return w.step("proof")
}
func (w *shiftWorld) EnqueuePublication(_ context.Context, i control.PublicationIntent) (control.Publication, bool, error) {
	w.intent = i
	if err := w.step("enqueue"); err != nil {
		return control.Publication{}, false, err
	}
	w.event = control.Publication{ID: i.ID, Kind: i.Kind, Status: "pending"}
	return w.event, true, nil
}
func (w *shiftWorld) Apply(context.Context, control.Publication) error { return w.step("apply") }
func (w *shiftWorld) ConfirmPublication(_ context.Context, e control.Publication, at time.Time) (control.Publication, error) {
	if err := w.step("confirm"); err != nil {
		return control.Publication{}, err
	}
	e.Status = "confirmed"
	e.ConfirmedAt = &at
	w.event = e
	return e, nil
}
func (w *shiftWorld) DrainShift(context.Context, control.Publication) (bool, error) {
	return !w.busy, w.step("drain")
}
func shiftFixture(t *testing.T) (update.Shifter, *shiftWorld, update.ReadabilityProof) {
	t.Helper()
	v, _, next, old := readabilityFixture(t)
	p, err := v.Check(t.Context(), updatePlan(), next, old)
	if err != nil {
		t.Fatal(err)
	}
	w := &shiftWorld{}
	return update.Shifter{Store: w, Outbox: w, Delivery: w, Clock: testkit.FakeClock{Time: p.CheckedAt}}, w, p
}
func TestT13_09UnconfirmedShiftNeverStartsDrain(t *testing.T) {
	for _, fail := range []string{"proof", "enqueue", "apply", "confirm"} {
		t.Run(fail, func(t *testing.T) {
			s, w, p := shiftFixture(t)
			w.fail = fail
			out, err := s.Shift(t.Context(), p)
			if err == nil || out.Drained {
				t.Fatal(out, err)
			}
			for _, call := range w.trace {
				if call == "drain" {
					t.Fatal("unconfirmed shift closed original admission")
				}
			}
			if (fail == "apply" || fail == "confirm") && out.Event.ID == "" {
				t.Fatal("lost pending event")
			}
		})
	}
}
func TestT13_10ConfirmedShiftWaitsForActualSQLDrain(t *testing.T) {
	s, w, p := shiftFixture(t)
	w.busy = true
	out, err := s.Shift(t.Context(), p)
	if err != nil || out.Drained || out.Event.Status != "confirmed" || w.intent.Kind != "route_shift" || *w.intent.Expected != p.Origin || w.intent.Target != p.Destination || !reflect.DeepEqual(w.trace, []string{"proof", "enqueue", "apply", "confirm", "drain"}) {
		t.Fatal(out, err, w.trace)
	}
	w.busy = false
	out, err = s.Shift(t.Context(), p)
	if err != nil || !out.Drained {
		t.Fatal(out, err)
	}
}
