//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

// A transport barrier delays a real target write. It preserves the production
// database proof and streaming-query interfaces and never fabricates results.
type pausedDatabase struct {
	*adapters.Postgres
	target  storage.Connection
	once    sync.Once
	entered chan struct{}
	resume  chan struct{}
}

func (p *pausedDatabase) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := p.Postgres.Begin(ctx, c)
	if err != nil || c != p.target {
		return tx, err
	}
	return pausedTransaction{Transaction: tx, owner: p}, nil
}

type pausedTransaction struct {
	storage.Transaction
	owner *pausedDatabase
}

func (p pausedTransaction) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	var err error
	if strings.HasPrefix(sql, "TRUNCATE TABLE source.") {
		p.owner.once.Do(func() {
			close(p.owner.entered)
			select {
			case <-p.owner.resume:
			case <-ctx.Done():
				err = ctx.Err()
			}
		})
	}
	if err != nil {
		return 0, err
	}
	return p.Transaction.Exec(ctx, sql, args...)
}
func (p pausedTransaction) Query(ctx context.Context, sql string, args ...any) (storage.Rows, error) {
	return p.Transaction.(storage.QueryExecutor).Query(ctx, sql, args...)
}
func (p pausedTransaction) CopyFrom(ctx context.Context, table string, columns []string, rows [][]any) (int64, error) {
	return p.Transaction.(storage.CopyExecutor).CopyFrom(ctx, table, columns, rows)
}
func admitTask(t *testing.T, f *runtimeFixture, mode, id string) {
	t.Helper()
	if _, err := f.wr.Control.Admit(f.e.ctx, control.Request{ID: id, IdempotencyKey: id, Mode: mode, RequestedBy: "synthetic", AcceptedAt: realClock{}.Now()}); err != nil {
		t.Fatal(err)
	}
}
func changedGrade(f *runtimeFixture) {
	f.source.mu.Lock()
	defer f.source.mu.Unlock()
	rows := f.source.rows["dwd_bzkscjzlb"]
	rows[1] = json.RawMessage(strings.Replace(string(rows[1]), `"KCCJ":90`, `"KCCJ":91`, 1))
}
func TestI03_08(t *testing.T) {
	f := newRuntime(t, true)
	body := `{"student_number":"001","academic_year":"all","page":1,"page_size":1}`
	_, token := f.queryOK(t, contract.StudentGrades, body, "")
	old, exists, err := f.wr.Active.Read(f.e.ctx)
	if err != nil || !exists {
		t.Fatal("missing initial active")
	}
	target, err := f.wc.Databases.Business(storage.Position{Instance: old.Position.Position.Instance, Slot: 1 - old.Position.Position.Slot})
	if err != nil {
		t.Fatal(err)
	}
	pause := &pausedDatabase{Postgres: f.wr.Database.(*adapters.Postgres), target: target, entered: make(chan struct{}), resume: make(chan struct{})}
	f.wr.Database = pause
	admitTask(t, f, "force_full", "next")
	ctx, cancel := context.WithCancel(f.e.ctx)
	done := make(chan error, 1)
	var resume sync.Once
	release := func() { resume.Do(func() { close(pause.resume) }) }
	finished := false
	t.Cleanup(func() {
		release()
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("worker barrier cleanup timed out")
			}
		}
	})
	go func() { done <- f.worker.Engine.Cycle(ctx) }()
	select {
	case <-pause.entered:
	case err := <-done:
		finished = true
		t.Fatalf("worker ended before second copy: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("second copy barrier not reached")
	}
	shifted, exists, err := f.wr.Active.Read(f.e.ctx)
	if err != nil || !exists || shifted.Position.BundleID != old.Position.BundleID || shifted.Position.Position.Instance == old.Position.Position.Instance {
		t.Fatalf("same-bundle shift missing: %v", err)
	}
	var open bool
	if err := f.e.connection(t, 4).QueryRow(f.e.ctx, "SELECT admission_open FROM control.slots WHERE instance=$1 AND slot=$2", string(old.Position.Position.Instance), old.Position.Position.Slot).Scan(&open); err != nil || open {
		t.Fatalf("old position admission not closed: %v", err)
	}
	second, retained := f.queryOK(t, contract.StudentGrades, strings.Replace(body, `"page":1`, `"page":2`, 1), token)
	items := second["items"].([]any)
	if retained != token || second["total"] != float64(2) || len(items) != 1 || !reflect.DeepEqual(items[0], expectedJSON(t, "grade_g02.json")) {
		t.Fatal("transfer invalidated combination pagination")
	}
	release()
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("worker failed to finish after transfer")
	}
}
func TestI03_09(t *testing.T) {
	f := newRuntime(t, true)
	body := `{"student_number":"001","academic_year":"all","score_min":80,"page":1,"page_size":1}`
	before, token := f.queryOK(t, contract.StudentGrades, body, "")
	changedGrade(f)
	f.run(t, "force_full", "next")
	// Force continuation to read the retained physical copy, not its old cache.
	keys, err := f.redis.raw.Do(f.e.ctx, "KEYS", f.redis.prefix+"query:Cinit:*")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys.([]any) {
		if _, err := f.redis.raw.Do(f.e.ctx, "DEL", key); err != nil {
			t.Fatal(err)
		}
	}
	retained, sameToken := f.queryOK(t, contract.StudentGrades, body, token)
	if sameToken != token || !reflect.DeepEqual(before, retained) {
		t.Fatal("old snapshot switched to new combination")
	}
	current, _ := f.queryOK(t, contract.StudentGrades, body, "")
	if current["items"].([]any)[0].(map[string]any)["score"] != float64(91) {
		t.Fatal("new publication did not contain changed grade")
	}
	conn := f.e.connection(t, 4)
	if _, err := conn.Exec(f.e.ctx, "UPDATE control.query_snapshots SET expires_at=created_at+interval '1 microsecond' WHERE token=$1", token); err != nil {
		t.Fatal(err)
	}
	tampered := "0" + token[1:]
	if tampered == token {
		tampered = "1" + token[1:]
	}
	for _, invalid := range []string{token, tampered} {
		status, envelope, _ := f.queryJSON(t, contract.StudentGrades, body, invalid, f.qc.Service.APIKey)
		if status != http.StatusConflict || envelope["data"] != nil {
			t.Fatalf("invalid snapshot silently switched: status=%d", status)
		}
	}
}
func TestI03_10(t *testing.T) {
	f := newRuntime(t, true)
	entered := make(chan string, 1)
	resume := make(chan struct{})
	var once, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(resume) }) }
	f.redis.beforeSet = func(ctx context.Context, key string) error {
		var err error
		once.Do(func() {
			entered <- key
			select {
			case <-resume:
			case <-ctx.Done():
				err = ctx.Err()
			}
		})
		return err
	}
	ctx, cancel := context.WithCancel(f.e.ctx)
	done := make(chan error, 1)
	finished := false
	t.Cleanup(func() {
		release()
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("late query cleanup timed out")
			}
		}
	})
	go func() {
		path := ""
		for _, d := range contract.Definitions() {
			if d.Operation == contract.StudentGrades {
				path = d.Path
			}
		}
		request, err := http.NewRequestWithContext(ctx, "POST", f.server.URL+path, strings.NewReader(allGrades))
		if err != nil {
			done <- err
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-API-Key", f.qc.Service.APIKey)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			done <- err
			return
		}
		defer response.Body.Close()
		var envelope map[string]any
		err = json.NewDecoder(response.Body).Decode(&envelope)
		if response.StatusCode != 200 || response.Header.Get(snapshot.Header) == "" {
			err = errors.New("late query failed")
		}
		done <- err
	}()
	var oldKey string
	select {
	case oldKey = <-entered:
	case err := <-done:
		finished = true
		t.Fatalf("query ended before cache fill: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("cache fill barrier not reached")
	}
	if !strings.HasPrefix(oldKey, "query:Cinit:") {
		t.Fatal("old query selected wrong combination")
	}
	changedGrade(f)
	f.run(t, "force_full", "next")
	release()
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("late query did not finish")
	}
	newKey := strings.Replace(oldKey, "query:Cinit:", "query:Cnext:", 1)
	oldValue, err := f.redis.Do(f.e.ctx, "GET", oldKey)
	if err != nil || oldValue == nil {
		t.Fatal("old cache fill missing", err)
	}
	newValue, err := f.redis.Do(f.e.ctx, "GET", newKey)
	if err != nil || newValue != nil {
		t.Fatal("old request wrote new combination cache", err)
	}
	current, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
	items := current["items"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["grade_id"] != "G02" || items[1].(map[string]any)["score"] != float64(91) {
		t.Fatal("new query result missing")
	}
	newValue, err = f.redis.Do(f.e.ctx, "GET", newKey)
	if err != nil || newValue == nil || reflect.DeepEqual(newValue, oldValue) {
		t.Fatal("new cache did not get independent changed result", err)
	}
}
