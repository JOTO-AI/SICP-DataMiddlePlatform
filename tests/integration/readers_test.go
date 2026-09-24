//go:build integration && linux

package integration

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/coord"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type queryProcessConfig struct {
	URLs    map[string]string
	Handle  read.Handle
	Process string
}

// This helper runs only through an explicitly selected child test. It uses the
// real query-role adapter and durable reader protocol, with its own OS process.
func TestQueryProcess(t *testing.T) {
	if os.Getenv("SICP_QUERY_CHILD") != "1" || os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Fatal("query child requires its GitHub integration parent")
	}
	input := json.NewDecoder(os.Stdin)
	output := json.NewEncoder(os.Stdout)
	var cfg queryProcessConfig
	if err := input.Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	registry, err := storage.LoadRegistry(func(k string) (string, bool) { v, ok := cfg.URLs[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := adapters.OpenPostgres(ctx, registry, 5*time.Second, cfg.Process)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	c := &coord.Coordinator{Store: control.Repository{Registry: registry, Database: db}, Proof: db, Clock: realClock{}, Random: rand.Reader, ProcessID: cfg.Process}
	request, err := c.AcquireFixed(ctx, cfg.Handle)
	if err != nil {
		t.Fatal(err)
	}
	requestContext := request.(interface {
		Context(context.Context) context.Context
	}).Context(ctx)
	sqlLease, err := c.BeginRead(requestContext, cfg.Handle)
	if err != nil {
		t.Fatal(err)
	}
	sqlContext := sqlLease.(interface {
		Context(context.Context) context.Context
	}).Context(ctx)
	connection, err := registry.Business(cfg.Handle.Position)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(sqlContext, connection)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var pid int
	if err := tx.QueryRow(sqlContext, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	running, stopSQL := context.WithCancel(sqlContext)
	defer stopSQL()
	finished := make(chan error, 1)
	go func() { _, err := tx.Exec(running, "SELECT pg_sleep(120)"); finished <- err }()
	if err := output.Encode(pid); err != nil {
		t.Fatal(err)
	}
	for {
		var command string
		if err := input.Decode(&command); err != nil {
			t.Fatal(err)
		}
		switch command {
		case "release_http":
			if err := request.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if err := sqlLease.Release(ctx); err == nil {
				t.Fatal("live SQL released")
			}
			if err := output.Encode("blocked"); err != nil {
				t.Fatal(err)
			}
		case "finish_sql":
			stopSQL()
			select {
			case err := <-finished:
				if err == nil {
					t.Fatal("long SQL unexpectedly completed")
				}
			case <-ctx.Done():
				t.Fatal("SQL cancellation did not finish")
			}
			// Cancellation can close the driver connection before rollback. The
			// production proof must consult PostgreSQL before finishing the lease.
			_ = tx.Rollback(ctx)
			deadline := time.Now().Add(5 * time.Second)
			for {
				err := sqlLease.Release(ctx)
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal(err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err := request.Close(ctx); err != nil {
				t.Fatal(err)
			}
			if err := output.Encode("finished"); err != nil {
				t.Fatal(err)
			}
			return
		default:
			t.Fatal("unknown child command")
		}
	}
}

type queryProcess struct {
	cmd      *exec.Cmd
	in       *json.Encoder
	out      *json.Decoder
	done     chan error
	finished bool
	pid      int
}

func startQueryProcess(t *testing.T, f *runtimeFixture, h read.Handle) *queryProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(f.e.ctx, executable, "-test.run=^TestQueryProcess$", "-test.timeout=100s")
	cmd.Env = append(os.Environ(), "SICP_QUERY_CHILD=1")
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, childOutput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = childOutput
	p := &queryProcess{cmd: cmd, in: json.NewEncoder(input), out: json.NewDecoder(output), done: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		output.Close()
		childOutput.Close()
		t.Fatal(err)
	}
	_ = childOutput.Close()
	t.Cleanup(func() {
		if !p.finished {
			_ = cmd.Process.Signal(syscall.SIGCONT)
			_ = p.in.Encode("finish_sql")
			select {
			case <-p.done:
			case <-time.After(8 * time.Second):
				_ = cmd.Process.Kill()
				<-p.done
			}
		}
		_ = input.Close()
		_ = output.Close()
	})
	go func() { p.done <- cmd.Wait() }()
	urls := map[string]string{"DATABASE_CONTROL_URL": f.qc.Databases.Control().DSN(), "DATABASE_LOG_URL": f.qc.Databases.Logs().DSN()}
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		for slot := 0; slot < 2; slot++ {
			c, err := f.qc.Databases.Business(storage.Position{Instance: instance, Slot: slot})
			if err != nil {
				t.Fatal(err)
			}
			urls[fmt.Sprintf("DATABASE_%s%d_URL", instance, slot)] = c.DSN()
		}
	}
	if err := p.in.Encode(queryProcessConfig{URLs: urls, Handle: h, Process: "child-" + rand.Text()}); err != nil {
		t.Fatal(err)
	}
	if err := p.out.Decode(&p.pid); err != nil {
		t.Fatalf("query child startup: %v", err)
	}
	conn := f.e.connection(t, 0)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var active bool
		if err := conn.QueryRow(f.e.ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND state='active' AND wait_event='PgSleep')", p.pid).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not execute real long SQL")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return p
}

func (p *queryProcess) command(t *testing.T, command, want string) {
	t.Helper()
	if err := p.in.Encode(command); err != nil {
		t.Fatal(err)
	}
	var result string
	if err := p.out.Decode(&result); err != nil || result != want {
		t.Fatalf("child %s: result=%q error=%v", command, result, err)
	}
}
func (p *queryProcess) finish(t *testing.T) {
	t.Helper()
	if err := p.cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	p.command(t, "finish_sql", "finished")
	select {
	case err := <-p.done:
		p.finished = true
		if err != nil {
			t.Fatalf("query child exit: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("query child did not exit")
	}
}

func nonServingHandle(t *testing.T, f *runtimeFixture) read.Handle {
	t.Helper()
	active, exists, err := f.wr.Active.Read(f.e.ctx)
	if err != nil || !exists || active.Position.Position.Instance != storage.Standby {
		t.Fatal("initial standby route required")
	}
	h := read.Handle{Position: storage.Position{Instance: storage.Main, Slot: 0}}
	if err := f.e.connection(t, 4).QueryRow(f.e.ctx, "SELECT generation,bundle_id FROM control.slots WHERE instance='M' AND slot=0 AND state='ready'").Scan(&h.Generation, &h.Combination); err != nil {
		t.Fatal(err)
	}
	return h
}
func ownedRepository(t *testing.T, f *runtimeFixture) control.Repository {
	t.Helper()
	r := taskFixture(t, f.e)
	owner, err := r.Acquire(f.e.ctx, "task", "integration-reader-check")
	if err != nil {
		t.Fatal(err)
	}
	return r.WithOwnership(owner)
}
func slotIdentity(h read.Handle) control.SlotIdentity {
	return control.SlotIdentity{Position: h.Position, Generation: h.Generation, BundleID: h.Combination}
}
func checkDrain(t *testing.T, f *runtimeFixture, r control.Repository, h read.Handle, want bool) {
	t.Helper()
	drained, err := r.Drained(f.e.ctx, slotIdentity(h))
	if err != nil || drained != want {
		t.Fatalf("drain=%v want=%v error=%v", drained, want, err)
	}
}
func longSQLScenario(t *testing.T, complete bool) {
	f := newRuntime(t, true)
	h := nonServingHandle(t, f)
	first := startQueryProcess(t, f, h)
	second := startQueryProcess(t, f, h)
	for _, p := range []*queryProcess{first, second} {
		p.command(t, "release_http", "blocked")
		if err := p.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
	}
	r := ownedRepository(t, f)
	if err := r.CloseAdmission(f.e.ctx, slotIdentity(h)); err != nil {
		t.Fatal(err)
	}
	checkDrain(t, f, r, h, false)
	if _, err := r.ReserveOverwrite(f.e.ctx, slotIdentity(h)); !errors.Is(err, control.ErrBusy) {
		t.Fatalf("live SQL overwrite: %v", err)
	}
	if complete {
		first.finish(t)
		checkDrain(t, f, r, h, false)
		second.finish(t)
		checkDrain(t, f, r, h, true)
		next, err := r.ReserveOverwrite(f.e.ctx, slotIdentity(h))
		if err != nil || next.Generation != h.Generation+1 {
			t.Fatalf("finished SQL overwrite: %v", err)
		}
	}
}
func TestI03_06(t *testing.T) { longSQLScenario(t, false) }
func TestI03_07(t *testing.T) { longSQLScenario(t, true) }
func TestI03_11(t *testing.T) {
	f := newRuntime(t, true)
	h := nonServingHandle(t, f)
	conn := f.e.connection(t, 4)
	var before int
	if err := conn.QueryRow(f.e.ctx, "SELECT count(*) FROM control.reader_sql").Scan(&before); err != nil {
		t.Fatal(err)
	}
	c := &coord.Coordinator{Store: f.qr.Control, Proof: f.qr.Database.(*adapters.Postgres), Clock: realClock{}, Random: rand.Reader, ProcessID: f.qr.ProcessID}
	lease, err := c.AcquireFixed(f.e.ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	oldContext := lease.(interface {
		Context(context.Context) context.Context
	}).Context(f.e.ctx)
	if err := lease.Close(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	r := ownedRepository(t, f)
	if err := r.CloseAdmission(f.e.ctx, slotIdentity(h)); err != nil {
		t.Fatal(err)
	}
	next, err := r.ReserveOverwrite(f.e.ctx, slotIdentity(h))
	if err != nil || next.Generation != h.Generation+1 {
		t.Fatal("replacement generation unavailable", err)
	}
	if _, err := c.BeginRead(oldContext, h); !errors.Is(err, control.ErrEntity) {
		t.Fatalf("old qualification accepted: %v", err)
	}
	if _, err := c.AcquireFixed(f.e.ctx, h); !errors.Is(err, control.ErrEntity) {
		t.Fatalf("old generation admitted: %v", err)
	}
	var count int
	if err := conn.QueryRow(f.e.ctx, "SELECT count(*) FROM control.reader_sql").Scan(&count); err != nil || count != before {
		t.Fatalf("rejected qualification started SQL: %d -> %d, %v", before, count, err)
	}
}
