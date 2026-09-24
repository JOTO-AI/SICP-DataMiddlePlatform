package runtimecontrol_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/runtimecontrol"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type row []any

func (r row) Scan(dst ...any) error {
	if len(r) != len(dst) {
		return errors.New("unexpected fixture query")
	}
	for n, v := range r {
		out := reflect.ValueOf(dst[n]).Elem()
		if v == nil {
			out.SetZero()
			continue
		}
		if out.Kind() == reflect.Pointer {
			out.Set(reflect.New(out.Type().Elem()))
			out = out.Elem()
		}
		value := reflect.ValueOf(v)
		if !value.CanConvert(out.Type()) {
			return errors.New("fixture column type does not match query")
		}
		out.Set(value.Convert(out.Type()))
	}
	return nil
}

type database struct {
	active             storage.Instance
	owner              int64
	generation         int64
	readers            int64
	admitted, pending  bool
	calls              []string
	commits, rollbacks int
	at                 time.Time
}

func (d *database) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	return d, nil
}
func (d *database) Exec(_ context.Context, q string, args ...any) (int64, error) {
	d.calls = append(d.calls, q)
	return 1, nil
}
func (d *database) QueryRow(_ context.Context, q string, args ...any) storage.Row {
	d.calls = append(d.calls, q)
	switch {
	case strings.Contains(q, "FROM control.execution"):
		return row{"task", "worker", d.owner}
	case strings.Contains(q, "FROM control.tasks"):
		return row{"task", "key", "incremental", "running", "operator", nil, d.at, d.at, nil, nil, nil, nil, "worker", d.owner, nil, "{}"}
	case strings.Contains(q, "status='pending'"):
		return row{d.pending}
	case strings.Contains(q, "target::text FROM control.publication_outbox"):
		a := control.ActiveMetadata{Sequence: 1, EventID: "old", TaskID: "old-task", OwnerGeneration: 1, Position: control.SlotIdentity{Position: storage.Position{Instance: d.active, Slot: 0}, Generation: 1, BundleID: "C100"}, CodeVersion: "v1"}
		raw, _ := control.ActiveDocument(a)
		return row{string(raw)}
	case strings.Contains(q, "FROM control.candidates"):
		return row{"candidate", "task", "S", int16(1), int64(2), "C101", d.owner, "building", nil, nil}
	case strings.Contains(q, "FROM control.slots"):
		return row{"S", int16(1), d.generation, "C101", d.admitted, "building"}
	case strings.Contains(q, "COUNT(*) FROM control.readers"):
		return row{d.readers}
	}
	return row{}
}
func (d *database) Commit(context.Context) error   { d.commits++; return nil }
func (d *database) Rollback(context.Context) error { d.rollbacks++; return nil }
func gateFixture(t *testing.T) (runtimecontrol.Store, *database, source.Target) {
	t.Helper()
	v := testkit.RuntimeValues("/data")
	c, e := config.LoadRuntime(config.Worker, func(k string) (string, bool) { s, ok := v[k]; return s, ok })
	if e != nil {
		t.Fatal(e)
	}
	d := &database{active: storage.Main, owner: 3, generation: 2, at: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)}
	s := runtimecontrol.Store{Repository: control.Repository{Registry: c.Databases, Database: d}, Owner: control.Ownership{TaskID: "task", OwnerID: "worker", Generation: 3}, Clock: testkit.FakeClock{Time: d.at}, CodeVersion: "v1"}
	return s, d, source.Target{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 2, CandidateID: "candidate"}
}
func TestT15_10RealBuildGateRejectsServingInstanceStaleFenceReadersAndPending(t *testing.T) {
	for _, scenario := range []string{"active", "owner", "generation", "reader", "admission", "pending"} {
		t.Run(scenario, func(t *testing.T) {
			s, d, target := gateFixture(t)
			switch scenario {
			case "active":
				d.active = storage.Standby
			case "owner":
				d.owner = 4
			case "generation":
				d.generation = 3
			case "reader":
				d.readers = 1
			case "admission":
				d.admitted = true
			case "pending":
				d.pending = true
			}
			lease, e := s.LockBuild(t.Context(), target)
			if e == nil || lease != nil || d.commits != 0 || d.rollbacks != 1 {
				t.Fatal(lease, e, d.commits, d.rollbacks)
			}
			for _, q := range d.calls {
				if strings.HasPrefix(q, "UPDATE") || strings.HasPrefix(q, "INSERT") {
					t.Fatal("unsafe gate mutated state")
				}
			}
		})
	}
}
func TestT15_10RealBuildGateRetainsLockUntilPhysicalWriterReleases(t *testing.T) {
	s, d, target := gateFixture(t)
	lease, e := s.LockBuild(t.Context(), target)
	if e != nil {
		t.Fatal(e)
	}
	v := lease.State()
	if !v.OwnerValid || v.ActiveInstance != storage.Main || v.Generation != 2 || v.AdmissionOpen || v.Readers != 0 || d.commits != 0 || d.rollbacks != 0 {
		t.Fatal(v)
	}
	if e = lease.Release(t.Context()); e != nil || d.commits != 1 {
		t.Fatal(e, d.commits)
	}
}
