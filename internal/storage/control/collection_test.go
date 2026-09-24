package control_test

import (
	"context"
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type collectionDB struct {
	mu                       sync.Mutex
	resource                 control.Resource
	refs                     map[string]bool
	busy, closed, failCommit bool
	active                   control.Document
	locks                    int
}
type collectionTX struct {
	d            *collectionDB
	resource     control.Resource
	refs         map[string]bool
	closed, done bool
}

func (d *collectionDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	d.mu.Lock()
	return &collectionTX{d: d, resource: d.resource, refs: maps.Clone(d.refs), closed: d.closed}, nil
}
func (x *collectionTX) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	switch {
	case strings.Contains(sql, "pg_advisory_xact_lock"):
		x.d.locks++
		return 1, nil
	case strings.HasPrefix(sql, "INSERT INTO control.resources "):
		if x.resource.ID == "" {
			x.resource = control.Resource{ID: args[0].(string), Kind: args[1].(string), BundleID: args[2].(string)}
		}
		return 1, nil
	case strings.HasPrefix(sql, "INSERT INTO control.resource_refs"):
		key := args[1].(string) + ":" + args[2].(string)
		released, exists := x.refs[key]
		if strings.Contains(sql, "VALUES ($1,$2,$3,TRUE)") {
			if exists && released {
				return 0, nil
			}
			x.refs[key] = true
			return 1, nil
		}
		if exists {
			return 0, nil
		}
		x.refs[key] = false
		return 1, nil
	case strings.HasPrefix(sql, "UPDATE control.resources SET revision="):
		x.resource.Revision++
		return 1, nil
	case strings.HasPrefix(sql, "UPDATE control.resources SET retired="):
		if x.resource.Revision != args[1] || x.resource.Retired {
			return 0, nil
		}
		x.resource.Retired = true
		x.resource.Revision++
		return 1, nil
	case strings.HasPrefix(sql, "UPDATE control.slots SET admission_open=FALSE"):
		x.closed = true
		return 2, nil
	default:
		return 0, errors.New("unexpected collection SQL")
	}
}
func (x *collectionTX) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	switch {
	case strings.Contains(sql, "FROM control.execution"):
		return result{raw: []any{"task-A", "owner-A", int64(9)}}
	case strings.HasPrefix(sql, "SELECT kind,bundle_id,revision,retired"):
		return result{raw: []any{x.resource.Kind, x.resource.BundleID, x.resource.Revision, x.resource.Retired}}
	case strings.HasPrefix(sql, "SELECT released FROM control.resource_refs"):
		return result{raw: []any{x.refs[args[1].(string)+":"+args[2].(string)]}}
	case strings.HasPrefix(sql, "SELECT EXISTS (SELECT 1 FROM control.resource_refs"):
		busy := x.d.busy
		for _, released := range x.refs {
			busy = busy || !released
		}
		return result{raw: []any{busy}}
	case strings.HasPrefix(sql, "SELECT (SELECT target::text"):
		return result{raw: []any{string(x.d.active)}}
	default:
		return result{fail: true}
	}
}
func (x *collectionTX) Commit(context.Context) error {
	if x.d.failCommit {
		return errors.New("commit failed")
	}
	x.d.resource = x.resource
	x.d.refs = x.refs
	x.d.closed = x.closed
	x.done = true
	x.d.mu.Unlock()
	return nil
}
func (x *collectionTX) Rollback(context.Context) error {
	if !x.done {
		x.done = true
		x.d.mu.Unlock()
	}
	return nil
}
func collectionRepository(t *testing.T) (control.Repository, *collectionDB) {
	r, _ := repository(t)
	active := control.ActiveMetadata{Sequence: 2, EventID: "new", TaskID: "new-task", OwnerGeneration: 1, Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Main, Slot: 1}, Generation: 2, BundleID: "C102"}, CodeVersion: "v1"}
	doc, _ := control.ActiveDocument(active)
	d := &collectionDB{refs: map[string]bool{}, active: doc}
	r.Database = d
	r = writer(r)
	if err := r.RegisterResource(t.Context(), control.Resource{ID: "old-version", Kind: "version", BundleID: "C100"}); err != nil {
		t.Fatal(err)
	}
	return r, d
}

func TestT14_14EachReferencePreventsCollectionAndReleaseIsTerminal(t *testing.T) {
	for _, kind := range []string{"reader", "pagination", "backfill", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			r, d := collectionRepository(t)
			if err := r.ResourceReference(t.Context(), d.resource.ID, "holder", kind, false); err != nil {
				t.Fatal(err)
			}
			if _, err := r.PlanCollection(t.Context(), d.resource.ID); err == nil || d.resource.Retired {
				t.Fatal("referenced resource eligible")
			}
			if err := r.ResourceReference(t.Context(), d.resource.ID, "holder", kind, true); err != nil {
				t.Fatal(err)
			}
			if err := r.ResourceReference(t.Context(), d.resource.ID, "holder", kind, false); err == nil {
				t.Fatal("late retain revived released owner")
			}
			plan, err := r.PlanCollection(t.Context(), d.resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			retired, err := r.RetireCollection(t.Context(), plan)
			if err != nil || !retired.Retired || !d.closed {
				t.Fatal(retired, err)
			}
			if err := r.ResourceReference(t.Context(), d.resource.ID, "late", kind, false); err == nil {
				t.Fatal("retired resource retained")
			}
		})
	}
}

func TestT14_15NewReferenceInvalidatesPlanEvenAfterItIsReleased(t *testing.T) {
	for _, release := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "released"}[release], func(t *testing.T) {
			r, d := collectionRepository(t)
			plan, err := r.PlanCollection(t.Context(), d.resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = r.ResourceReference(t.Context(), d.resource.ID, "late", "backfill", false); err != nil {
				t.Fatal(err)
			}
			if release {
				if err = r.ResourceReference(t.Context(), d.resource.ID, "late", "backfill", true); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = r.RetireCollection(t.Context(), plan); err == nil || d.resource.Retired || d.closed {
				t.Fatal("stale plan deleted resource")
			}
		})
	}
}

func TestT14_14CollectionRechecksLiveDatabaseReferencesAndCurrentVersion(t *testing.T) {
	for _, failure := range []string{"references", "current", "commit"} {
		t.Run(failure, func(t *testing.T) {
			r, d := collectionRepository(t)
			plan, err := r.PlanCollection(t.Context(), d.resource.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "references":
				d.busy = true
			case "current":
				active, _ := control.DecodeActive(d.active)
				active.Position.BundleID = d.resource.BundleID
				d.active, _ = control.ActiveDocument(active)
			case "commit":
				d.failCommit = true
			}
			if out, err := r.RetireCollection(t.Context(), plan); err == nil || out.ID != "" || d.resource.Retired || d.closed {
				t.Fatal("deletion gate bypassed", out, err)
			}
		})
	}
}

type retiredFiles struct {
	calls int
	fail  bool
}

func (f *retiredFiles) DeleteRetired(_ context.Context, r control.Resource) error {
	if !r.Retired {
		return errors.New("not retired")
	}
	f.calls++
	if f.fail {
		return errors.New("delete failed")
	}
	return nil
}
func TestT14_15PhysicalDeletionRequiresCommittedRetirementAndCanRetry(t *testing.T) {
	r, d := collectionRepository(t)
	plan, err := r.PlanCollection(t.Context(), d.resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	files := &retiredFiles{fail: true}
	c := update.Collector{Store: r, Files: files}
	d.failCommit = true
	if err = c.Collect(t.Context(), plan); err == nil || files.calls != 0 {
		t.Fatal("delete before retirement commit")
	}
	d.failCommit = false
	if err = c.Collect(t.Context(), plan); err == nil || files.calls != 1 || !d.resource.Retired {
		t.Fatal("lost retirement on deletion failure")
	}
	files.fail = false
	if err = c.Collect(t.Context(), plan); err != nil || files.calls != 2 {
		t.Fatal("failed retirement replay", err)
	}
}

func TestT14_15ReleaseBeforeRetainCreatesTerminalTombstone(t *testing.T) {
	r, d := collectionRepository(t)
	if err := r.ResourceReference(t.Context(), d.resource.ID, "late", "recovery", true); err != nil {
		t.Fatal(err)
	}
	if err := r.ResourceReference(t.Context(), d.resource.ID, "late", "recovery", false); err == nil {
		t.Fatal("release-before-retain resurrected reference")
	}
}

func TestT14_15ReferenceAdmissionAndRetirementAreMutuallyExclusive(t *testing.T) {
	r, d := collectionRepository(t)
	plan, err := r.PlanCollection(t.Context(), d.resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := d.resource.ID
	var wg sync.WaitGroup
	wg.Add(2)
	var retained, retired bool
	go func() {
		defer wg.Done()
		retained = r.ResourceReference(t.Context(), id, "concurrent", "recovery", false) == nil
	}()
	go func() { defer wg.Done(); _, e := r.RetireCollection(t.Context(), plan); retired = e == nil }()
	wg.Wait()
	if retained == retired {
		t.Fatal("reference and retirement must have exactly one winner", retained, retired)
	}
}
