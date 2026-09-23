package control_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

// Transactional substitute models the shared fixed control lock and rollback.
// Assertions below separately require the production SQL lock and predicates.
type readerDB struct {
	mu         sync.Mutex
	slot       control.Slot
	readers    int
	calls      []dbCall
	failCommit bool
}
type readerTX struct {
	db      *readerDB
	before  control.Slot
	readers int
	done    bool
	locked  bool
}

func (d *readerDB) Begin(_ context.Context, _ storage.Connection) (storage.Transaction, error) {
	d.mu.Lock()
	return &readerTX{db: d, before: d.slot, readers: d.readers}, nil
}
func (t *readerTX) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	t.db.calls = append(t.db.calls, dbCall{sql, args})
	if strings.Contains(sql, "pg_advisory_xact_lock") {
		if len(args) != 2 || args[0] != int32(734219002) || args[1] != int32(3) {
			return 0, errors.New("wrong slot lock")
		}
		t.locked = true
		return 1, nil
	}
	if !t.locked {
		return 0, errors.New("mutation without shared slot lock")
	}
	switch {
	case strings.HasPrefix(sql, "INSERT INTO control.readers"):
		t.db.readers++
		return 1, nil
	case strings.HasPrefix(sql, "UPDATE control.slots"):
		t.db.slot.AdmissionOpen = false
		if strings.Contains(sql, "state='building'") {
			t.db.slot.State = "building"
			t.db.slot.Generation = args[2].(int64)
			t.db.slot.BundleID = nil
		}
		return 1, nil
	default:
		return 0, errors.New("unexpected write")
	}
}
func (t *readerTX) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	t.db.calls = append(t.db.calls, dbCall{sql, args})
	if strings.Contains(sql, "FROM control.execution") {
		return result{raw: []any{"task-A", "owner-A", int64(9)}}
	}
	if !t.locked {
		return result{fail: true}
	}
	if strings.Contains(sql, "FROM control.slots") {
		s := t.db.slot
		var b any
		if s.BundleID != nil {
			b = *s.BundleID
		}
		return result{raw: []any{s.Generation, b, s.AdmissionOpen, s.State}}
	}
	if strings.Contains(sql, "SELECT EXISTS") {
		if !strings.Contains(sql, "released_at IS NULL OR sql_finished_at IS NULL") || strings.Contains(sql, "generation=") {
			return result{fail: true}
		}
		return result{raw: []any{t.db.readers > 0}}
	}
	return result{fail: true}
}
func (t *readerTX) Commit(context.Context) error {
	if t.db.failCommit {
		return errors.New("commit failed")
	}
	t.done = true
	t.db.mu.Unlock()
	return nil
}
func (t *readerTX) Rollback(context.Context) error {
	if !t.done {
		t.db.slot = t.before
		t.db.readers = t.readers
		t.done = true
		t.db.mu.Unlock()
	}
	return nil
}
func readerRepository(t *testing.T) (control.Repository, *readerDB, control.SlotIdentity) {
	r, _ := repository(t)
	d := &readerDB{slot: control.Slot{Instance: "S", Slot: 1, Generation: 7, BundleID: ptr("C1"), AdmissionOpen: true, State: "ready"}}
	r.Database = d
	return r, d, control.SlotIdentity{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 7, BundleID: "C1"}
}
func writer(r control.Repository) control.Repository {
	return r.WithOwnership(control.Ownership{TaskID: "task-A", OwnerID: "owner-A", Generation: 9})
}

func TestT11_10ClosedAdmissionRejectsLateOldPointer(t *testing.T) {
	r, d, h := readerRepository(t)
	if err := writer(r).CloseAdmission(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	got, err := r.RegisterReader(t.Context(), h, "reader", "query-process", time.Now())
	if !errors.Is(err, control.ErrBusy) || got.ID != "" || d.readers != 0 {
		t.Fatal(got, err)
	}
	if _, err = writer(r).ReserveOverwrite(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	if _, err = r.RegisterReader(t.Context(), h, "late", "query-process", time.Now()); err == nil {
		t.Fatal("old generation admitted")
	}
}

func TestT11_11RegisterAndOverwriteAreMutuallyExclusive(t *testing.T) {
	for range 30 {
		r, d, h := readerRepository(t)
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() {
			<-start
			_, err := r.RegisterReader(context.Background(), h, "reader", "query-process", time.Now())
			results <- err
		}()
		go func() { <-start; _, err := writer(r).ReserveOverwrite(context.Background(), h); results <- err }()
		close(start)
		a, b := <-results, <-results
		if (a == nil) == (b == nil) {
			t.Fatalf("expected exactly one winner: %v %v", a, b)
		}
		if d.readers > 0 && d.slot.State == "building" {
			t.Fatal("active reader overwritten")
		}
	}
}

func TestT11_11CommitFailureGrantsNeitherReaderNorReplacement(t *testing.T) {
	r, d, h := readerRepository(t)
	d.failCommit = true
	reader, err := r.RegisterReader(t.Context(), h, "reader", "query-process", time.Now())
	if err == nil || reader.ID != "" || d.readers != 0 {
		t.Fatal(reader, err)
	}
	slot, err := writer(r).ReserveOverwrite(t.Context(), h)
	if err == nil || slot.Generation != 0 || d.slot.Generation != 7 {
		t.Fatal(slot, err)
	}
	if err = r.CloseAdmission(t.Context(), h); !errors.Is(err, control.ErrOwnership) {
		t.Fatal("unfenced writer", err)
	}
}

func TestT11_11ExistingReaderSurvivesCloseAndBlocksOverwrite(t *testing.T) {
	r, d, h := readerRepository(t)
	if _, err := r.RegisterReader(t.Context(), h, "reader", "query-process", time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := writer(r).CloseAdmission(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	if _, err := writer(r).ReserveOverwrite(t.Context(), h); !errors.Is(err, control.ErrBusy) || d.readers != 1 || d.slot.Generation != 7 || d.slot.AdmissionOpen {
		t.Fatal("reader lost after closure", err, d.slot)
	}
}
