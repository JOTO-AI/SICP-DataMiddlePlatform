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

// This substitute implements transactional raw rows and uniqueness, not the
// service's admission/start decisions. Assertions exercise the actual repository.
type taskDB struct {
	mu                   sync.Mutex
	records              map[string][]any
	owner                control.Ownership
	lastStarted, nextDue *time.Time
	revision             int64
	deferred, pending    bool
	cooldown             *time.Time
	failSQL              string
	failCommit, failRead bool
	rollbacks            int
	calls                []dbCall
	mutations            []dbCall
}
type taskTx struct {
	db                   *taskDB
	records              map[string][]any
	owner                control.Ownership
	lastStarted, nextDue *time.Time
	revision             int64
	deferred             bool
	closed               bool
	mutations            []dbCall
}

func (d *taskDB) Begin(_ context.Context, c storage.Connection) (storage.Transaction, error) {
	if !strings.Contains(c.DSN(), "/control?") {
		return nil, errors.New("wrong database")
	}
	d.mu.Lock()
	x := &taskTx{db: d, records: map[string][]any{}, owner: d.owner, lastStarted: d.lastStarted, nextDue: d.nextDue, revision: d.revision, deferred: d.deferred}
	for key, row := range d.records {
		x.records[key] = append([]any(nil), row...)
	}
	return x, nil
}
func (x *taskTx) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	x.db.calls = append(x.db.calls, dbCall{sql, args})
	if x.db.failSQL != "" && strings.Contains(sql, x.db.failSQL) {
		return 0, errors.New("injected SQL failure")
	}
	switch {
	case strings.HasPrefix(sql, "UPDATE control.schedule SET deferred=TRUE"):
		x.deferred = true
		return 1, nil
	case strings.HasPrefix(sql, "UPDATE control.execution SET task_id=$1"):
		if x.owner.TaskID != "" || x.owner.Generation != args[3].(int64) {
			return 0, nil
		}
		x.owner = control.Ownership{TaskID: args[0].(string), OwnerID: args[1].(string), Generation: args[2].(int64)}
		return 1, nil
	case strings.HasPrefix(sql, "UPDATE control.tasks SET owner_id=$1"):
		for _, row := range x.records {
			if row[0] == args[2] && row[13] == args[3] {
				row[12] = args[0]
				row[13] = args[1]
				return 1, nil
			}
		}
		return 0, nil
	case strings.HasPrefix(sql, "UPDATE control.tasks SET owner_id=NULL"):
		for _, row := range x.records {
			if row[0] == args[0] && row[12] == args[1] && row[13] == args[2] {
				row[12] = nil
				return 1, nil
			}
		}
		return 0, nil
	case strings.HasPrefix(sql, "UPDATE control.execution SET task_id=NULL"):
		if x.owner.TaskID != args[0] || x.owner.OwnerID != args[1] || x.owner.Generation != args[2] {
			return 0, nil
		}
		x.owner.TaskID = ""
		x.owner.OwnerID = ""
		return 1, nil
	case strings.HasPrefix(sql, "INSERT INTO control.candidates"), strings.HasPrefix(sql, "UPDATE control.candidates"), strings.HasPrefix(sql, "INSERT INTO control.source_progress"), strings.HasPrefix(sql, "UPDATE control.source_progress"):
		x.mutations = append(x.mutations, dbCall{sql, append([]any(nil), args...)})
		return 1, nil
	case strings.HasPrefix(sql, "INSERT INTO control.tasks"):
		key := args[1].(string)
		if _, ok := x.records[key]; ok {
			return 0, nil
		}
		x.records[key] = []any{args[0], args[1], args[2], "accepted", args[3], args[4], args[5], nil, nil, nil, nil, nil, nil, int64(0), nil, "{}"}
		return 1, nil
	case strings.HasPrefix(sql, "UPDATE control.tasks SET started_at"):
		for _, row := range x.records {
			if row[0] == args[1] && row[12] == args[2] && row[13] == args[3] && row[7] == nil {
				row[7] = args[0]
				row[3] = "running"
				return 1, nil
			}
		}
		return 0, nil
	case strings.HasPrefix(sql, "UPDATE control.schedule"):
		at := args[0].(time.Time)
		if x.lastStarted != nil && at.Before(*x.lastStarted) {
			return 0, nil
		}
		x.lastStarted = &at
		next := args[1].(time.Time)
		x.nextDue = &next
		x.revision++
		x.deferred = false
		return 1, nil
	default:
		return 0, errors.New("unexpected SQL in task test")
	}
}
func (x *taskTx) QueryRow(_ context.Context, sql string, args ...any) storage.Row {
	x.db.calls = append(x.db.calls, dbCall{sql, args})
	if strings.HasPrefix(sql, "SELECT revision,last_started_at") {
		value := func(at *time.Time) any {
			if at == nil {
				return nil
			}
			return *at
		}
		return result{raw: []any{x.revision, value(x.lastStarted), value(x.nextDue), x.deferred, value(x.db.cooldown)}}
	}
	if strings.Contains(sql, "SELECT EXISTS (SELECT 1 FROM control.tasks WHERE idempotency_key=") {
		_, ok := x.records[args[0].(string)]
		return result{raw: []any{ok}}
	}
	if strings.Contains(sql, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox") {
		return result{raw: []any{x.db.pending}}
	}
	if strings.Contains(sql, "SELECT EXISTS (SELECT 1 FROM control.tasks WHERE status IN") {
		busy := false
		for _, row := range x.records {
			switch row[3] {
			case "accepted", "running", "published_pending_progress", "recovery_pending":
				busy = true
			}
		}
		return result{raw: []any{busy}}
	}
	if strings.Contains(sql, "FROM control.execution") {
		var task, owner any
		if x.owner.TaskID != "" {
			task = x.owner.TaskID
		}
		if x.owner.OwnerID != "" {
			owner = x.owner.OwnerID
		}
		return result{raw: []any{task, owner, x.owner.Generation}, fail: x.db.failRead}
	}
	if strings.Contains(sql, `"idempotency_key"=$1`) {
		return result{raw: x.records[args[0].(string)], fail: x.db.failRead}
	}
	for _, row := range x.records {
		if row[0] == args[0] {
			return result{raw: row, fail: x.db.failRead}
		}
	}
	return result{}
}
func (x *taskTx) Commit(context.Context) error {
	if x.db.failCommit {
		return errors.New("injected commit failure")
	}
	x.db.records = x.records
	x.db.owner = x.owner
	x.db.lastStarted = x.lastStarted
	x.db.nextDue = x.nextDue
	x.db.revision = x.revision
	x.db.deferred = x.deferred
	x.db.mutations = append(x.db.mutations, x.mutations...)
	x.closed = true
	x.db.mu.Unlock()
	return nil
}
func (x *taskTx) Rollback(context.Context) error {
	if !x.closed {
		x.db.rollbacks++
		x.closed = true
		x.db.mu.Unlock()
	}
	return nil
}

func taskRepository(t *testing.T) (control.Repository, *taskDB, control.Request) {
	r, _ := repository(t)
	d := &taskDB{records: map[string][]any{}}
	r.Database = d
	req := control.Request{ID: "task-new", IdempotencyKey: "request-001", Mode: "incremental", RequestedBy: "admin-1", AcceptedAt: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)}
	return r, d, req
}
func grantFixture(d *taskDB, req control.Request) control.Ownership {
	owner := control.Ownership{TaskID: req.ID, OwnerID: "worker-1", Generation: 7}
	d.owner = owner
	d.records[req.IdempotencyKey][12] = owner.OwnerID
	d.records[req.IdempotencyKey][13] = owner.Generation
	return owner
}

func TestT02_09AcceptanceIsIdempotentAndDoesNotStartOrReschedule(t *testing.T) {
	r, d, req := taskRepository(t)
	next := req.AcceptedAt.Add(time.Hour)
	d.nextDue = &next
	d.revision = 4
	first, err := r.Accept(t.Context(), req)
	if err != nil || !first.Created || first.Task.ID != req.ID || first.Task.StartedAt != nil {
		t.Fatalf("acceptance: %+v %v", first, err)
	}
	retry := req
	retry.ID = "unused-retry-id"
	retry.AcceptedAt = retry.AcceptedAt.Add(time.Minute)
	second, err := r.Accept(t.Context(), retry)
	if err != nil || second.Created || second.Task.ID != req.ID || second.Task.AcceptedAt != req.AcceptedAt || second.Task.StartedAt != nil || len(d.records) != 1 {
		t.Fatal("retry created/started a different task")
	}
	if d.revision != 4 || d.lastStarted != nil || *d.nextDue != next {
		t.Fatal("acceptance changed actual-start schedule")
	}
	for _, call := range d.calls {
		if strings.HasPrefix(call.sql, "UPDATE") || strings.Contains(call.sql, "SET started_at") {
			t.Fatal("acceptance executed lifecycle writes")
		}
	}
	retry.Mode = "force_full"
	if _, err := r.Accept(t.Context(), retry); !errors.Is(err, control.ErrIdempotency) || len(d.records) != 1 {
		t.Fatal("different request reused idempotency key")
	}
}

func TestT02_09ActualStartRequiresOwnershipAndIsNotResetByRetry(t *testing.T) {
	r, d, req := taskRepository(t)
	if _, err := r.Accept(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	owner := control.Ownership{TaskID: req.ID, OwnerID: "worker-1", Generation: 7}
	start := req.AcceptedAt.Add(10 * time.Minute)
	if _, err := r.Start(t.Context(), owner, start); !errors.Is(err, control.ErrOwnership) || d.records[req.IdempotencyKey][7] != nil {
		t.Fatal("start without execution ownership")
	}
	owner = grantFixture(d, req)
	actual, err := r.Start(t.Context(), owner, start)
	if err != nil || actual != start || *d.lastStarted != start || *d.nextDue != start.Add(2*time.Hour) || d.revision != 1 {
		t.Fatal("start and schedule are not coherent", err)
	}
	actual, err = r.Start(t.Context(), owner, start.Add(time.Hour))
	if err != nil || actual != start || d.revision != 1 || *d.nextDue != start.Add(2*time.Hour) {
		t.Fatal("same task restart reset its start/schedule")
	}
	replayed, err := r.Accept(t.Context(), req)
	if err != nil || replayed.Created || replayed.Task.StartedAt == nil || *replayed.Task.StartedAt != start || d.revision != 1 {
		t.Fatal("repeated admission reset an already started task")
	}
}

func TestT02_12AcceptanceAndStartRollbackAllPartialState(t *testing.T) {
	for _, failure := range []string{"read", "commit"} {
		r, d, req := taskRepository(t)
		d.failRead = failure == "read"
		d.failCommit = failure == "commit"
		if accepted, err := r.Accept(t.Context(), req); err == nil || accepted.Task.ID != "" || accepted.Created || len(d.records) != 0 || d.rollbacks != 1 {
			t.Fatal("partial acceptance confirmed")
		}
	}
	for _, failure := range []string{"schedule", "commit", "backward_clock"} {
		r, d, req := taskRepository(t)
		if _, err := r.Accept(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		owner := grantFixture(d, req)
		if failure == "schedule" {
			d.failSQL = "UPDATE control.schedule"
		}
		d.failCommit = failure == "commit"
		var prior *time.Time
		if failure == "backward_clock" {
			future := req.AcceptedAt.Add(time.Hour)
			prior = &future
			d.lastStarted = prior
		}
		if started, err := r.Start(t.Context(), owner, req.AcceptedAt.Add(time.Minute)); err == nil || !started.IsZero() || d.records[req.IdempotencyKey][7] != nil || d.revision != 0 || d.lastStarted != prior {
			t.Fatal("partial actual start escaped rollback")
		}
		if d.records[req.IdempotencyKey][3] != "accepted" {
			t.Fatal("failed start changed task status")
		}
	}
}
