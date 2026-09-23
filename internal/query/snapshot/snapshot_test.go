package snapshot_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type store struct {
	values         map[string]control.Snapshot
	putErr, getErr error
}

func (s *store) PutSnapshot(_ context.Context, v control.Snapshot) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.values[v.Token] = v
	return nil
}
func (s *store) GetSnapshot(_ context.Context, k string) (control.Snapshot, error) {
	if s.getErr != nil {
		return control.Snapshot{}, s.getErr
	}
	v, ok := s.values[k]
	if !ok {
		return v, control.ErrMissing
	}
	return v, nil
}

type routes struct {
	active      read.Handle
	available   map[string]read.Handle
	activeCalls int
}

func (r *routes) Active(context.Context) (read.Handle, error) { r.activeCalls++; return r.active, nil }
func (r *routes) ForCombination(_ context.Context, c string) (read.Handle, error) {
	h, ok := r.available[c]
	if !ok {
		return h, read.ErrHandle
	}
	return h, nil
}
func fixture() (snapshot.Manager, *store, *routes, *testkit.FakeClock) {
	h := read.Handle{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 7, Combination: "C1"}
	s := &store{values: map[string]control.Snapshot{}}
	r := &routes{active: h, available: map[string]read.Handle{"C1": h}}
	clock := &testkit.FakeClock{Time: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)}
	return snapshot.Manager{Store: s, Routes: r, Clock: clock, Random: testkit.FixedRandom(2)}, s, r, clock
}
func request(t *testing.T, body string) contract.StudentGradesRequest {
	t.Helper()
	v, err := transport.Parse(contract.StudentGrades, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return v.(contract.StudentGradesRequest)
}

func TestT11_14SnapshotSurvivesSameCombinationMoveWithoutExtendingExpiry(t *testing.T) {
	m, _, r, clock := fixture()
	req := request(t, `{"student_number":"001"}`)
	first, err := m.Select(t.Context(), contract.StudentGrades, req, "")
	if err != nil {
		t.Fatal(err)
	}
	moved := first.Handle
	moved.Position = storage.Position{Instance: storage.Standby, Slot: 1}
	moved.Generation = 9
	r.available["C1"] = moved
	r.active = moved
	r.active.Combination = "C2"
	clock.Time = clock.Time.Add(time.Minute)
	page := 2
	req.Page = &page
	next, err := m.Select(t.Context(), contract.StudentGrades, req, first.Token)
	if err != nil || next.Handle != moved || next.Token != first.Token || next.ExpiresAt != first.ExpiresAt || r.activeCalls != 1 {
		t.Fatal(next, err, r.activeCalls)
	}
	a, _ := cache.Key(first.Handle.Combination, contract.StudentGrades, req)
	b, _ := cache.Key(next.Handle.Combination, contract.StudentGrades, req)
	if a != b {
		t.Fatal("physical move changed cache identity")
	}
	delete(r.available, "C1")
	if _, err = m.Select(t.Context(), contract.StudentGrades, req, first.Token); !errors.Is(err, snapshot.ErrSnapshot) || r.activeCalls != 1 {
		t.Fatal("silently switched to C2", err)
	}
}

func TestT11_16TamperingExpiryAndChangedConditionsRejectContinuation(t *testing.T) {
	for _, kind := range []string{"tamper", "malformed", "expired", "clock-backward", "student", "filter", "page-size", "operation"} {
		t.Run(kind, func(t *testing.T) {
			m, _, _, clock := fixture()
			req := request(t, `{"student_number":"001"}`)
			first, err := m.Select(t.Context(), contract.StudentGrades, req, "")
			if err != nil {
				t.Fatal(err)
			}
			token := first.Token
			op := contract.StudentGrades
			switch kind {
			case "tamper":
				token = token[:63] + "3"
			case "malformed":
				token = "bad"
			case "expired":
				clock.Time = first.ExpiresAt
			case "clock-backward":
				clock.Time = clock.Time.Add(-time.Second)
			case "student":
				req.StudentNumber = "002"
			case "filter":
				v := "C1"
				req.CourseCode = &v
			case "page-size":
				v := 200
				req.PageSize = &v
			case "operation":
				op = contract.BatchGrades
			}
			if got, err := m.Select(t.Context(), op, req, token); !errors.Is(err, snapshot.ErrSnapshot) || got.Token != "" {
				t.Fatal(got, err)
			}
		})
	}
}

func TestT11_16UnconfirmedPersistenceOrOutageNeverGrantsSnapshot(t *testing.T) {
	m, s, _, _ := fixture()
	failure := errors.New("control unavailable")
	s.putErr = failure
	req := request(t, `{"student_number":"001"}`)
	if got, err := m.Select(t.Context(), contract.StudentGrades, req, ""); !errors.Is(err, failure) || got.Token != "" {
		t.Fatal(got, err)
	}
	s.putErr = nil
	first, err := m.Select(t.Context(), contract.StudentGrades, req, "")
	if err != nil {
		t.Fatal(err)
	}
	s.getErr = failure
	if _, err = m.Select(t.Context(), contract.StudentGrades, req, first.Token); !errors.Is(err, failure) {
		t.Fatal("outage misclassified", err)
	}
}
