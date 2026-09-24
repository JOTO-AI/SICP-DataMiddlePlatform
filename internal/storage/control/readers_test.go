package control_test

import (
	"context"
	"errors"
	"maps"
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
	records    map[string]control.Reader
	executions map[string]sqlRecord
	retained   bool
	otherCopy  bool
}
type sqlRecord struct {
	reader   string
	started  time.Time
	finished *time.Time
}
type readerTX struct {
	db         *readerDB
	before     control.Slot
	readers    int
	done       bool
	locked     bool
	records    map[string]control.Reader
	executions map[string]sqlRecord
}

func (d *readerDB) Begin(_ context.Context, _ storage.Connection) (storage.Transaction, error) {
	d.mu.Lock()
	return &readerTX{db: d, before: d.slot, readers: d.readers, records: maps.Clone(d.records), executions: maps.Clone(d.executions)}, nil
}
func (t *readerTX) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	t.db.calls = append(t.db.calls, dbCall{sql, args})
	if strings.Contains(sql, "pg_advisory_xact_lock") {
		if len(args) == 1 && args[0] == int64(734219003) {
			return 1, nil
		}
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
		if t.db.records == nil {
			t.db.records = map[string]control.Reader{}
		}
		at := args[6].(time.Time)
		t.db.records[args[0].(string)] = control.Reader{ID: args[0].(string), Instance: args[1].(string), Slot: args[2].(int16), Generation: args[3].(int64), BundleID: args[4].(string), ProcessID: args[5].(string), AdmittedAt: at, SQLFinishedAt: &at}
		return 1, nil
	case strings.HasPrefix(sql, "INSERT INTO control.reader_sql"):
		if t.db.executions == nil {
			t.db.executions = map[string]sqlRecord{}
		}
		id := args[0].(string)
		if _, ok := t.db.executions[id]; ok {
			return 0, errors.New("duplicate execution")
		}
		t.db.executions[id] = sqlRecord{reader: args[1].(string), started: args[2].(time.Time)}
		return 1, nil
	case strings.HasPrefix(sql, "UPDATE control.reader_sql"):
		id := args[0].(string)
		record := t.db.executions[id]
		at := args[2].(time.Time)
		record.finished = &at
		t.db.executions[id] = record
		return 1, nil
	case strings.HasPrefix(sql, "UPDATE control.readers"):
		id := args[0].(string)
		record := t.db.records[id]
		if strings.Contains(sql, "released_at=$2") {
			at := args[1].(time.Time)
			record.ReleasedAt = &at
		} else if strings.Contains(sql, "sql_finished_at=NULL") {
			record.SQLFinishedAt = nil
		} else {
			at := args[1].(time.Time)
			record.SQLFinishedAt = &at
		}
		t.db.records[id] = record
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
	if strings.Contains(sql, "FROM control.resource_refs") {
		return result{raw: []any{false}}
	}
	if strings.Contains(sql, "FROM control.execution") {
		return result{raw: []any{"task-A", "owner-A", int64(9)}}
	}
	if strings.Contains(sql, "FROM control.query_snapshots") {
		if !strings.Contains(sql, "expires_at>CURRENT_TIMESTAMP") || !strings.Contains(sql, "s.instance<>$2 OR s.slot<>$3") {
			return result{fail: true}
		}
		return result{raw: []any{t.db.retained && !t.db.otherCopy}}
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
	if strings.Contains(sql, "FROM control.reader_sql") {
		if strings.Contains(sql, "SELECT EXISTS") {
			active := false
			for _, record := range t.db.executions {
				if record.reader == args[0].(string) && record.finished == nil {
					active = true
				}
			}
			return result{raw: []any{active}}
		}
		record, ok := t.db.executions[args[0].(string)]
		if !ok || record.reader != args[1].(string) {
			return result{fail: true}
		}
		var at any
		if record.finished != nil {
			at = *record.finished
		}
		return result{raw: []any{record.started, at}}
	}
	if strings.Contains(sql, "FROM control.readers WHERE id=$1") {
		r, ok := t.db.records[args[0].(string)]
		if !ok {
			return result{fail: true}
		}
		var released, finished any
		if r.ReleasedAt != nil {
			released = *r.ReleasedAt
		}
		if r.SQLFinishedAt != nil {
			finished = *r.SQLFinishedAt
		}
		return result{raw: []any{r.Instance, r.Slot, r.Generation, r.BundleID, r.AdmittedAt, released, finished}}
	}
	if strings.Contains(sql, "SELECT EXISTS") {
		if !strings.Contains(sql, "released_at IS NULL OR sql_finished_at IS NULL") || strings.Contains(sql, "generation=") {
			return result{fail: true}
		}
		busy := false
		for _, r := range t.db.records {
			if r.ReleasedAt == nil || r.SQLFinishedAt == nil {
				busy = true
			}
		}
		return result{raw: []any{busy}}
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
		t.db.records = t.records
		t.db.executions = t.executions
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
