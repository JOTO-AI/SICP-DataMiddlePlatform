//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/coord"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type writeFaultDatabase struct {
	*adapters.Postgres
	target    storage.Connection
	once      sync.Once
	fault     func(context.Context, storage.Transaction) error
	triggered bool
}

func (p *writeFaultDatabase) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	tx, err := p.Postgres.Begin(ctx, c)
	if err != nil || c != p.target {
		return tx, err
	}
	return writeFaultTransaction{Transaction: tx, owner: p}, nil
}

type writeFaultTransaction struct {
	storage.Transaction
	owner *writeFaultDatabase
}

func (p writeFaultTransaction) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	var err error
	if strings.HasPrefix(sql, "TRUNCATE TABLE source.") {
		p.owner.once.Do(func() { p.owner.triggered = true; err = p.owner.fault(ctx, p.Transaction) })
	}
	if err != nil {
		return 0, err
	}
	return p.Transaction.Exec(ctx, sql, args...)
}
func (p writeFaultTransaction) Query(ctx context.Context, sql string, args ...any) (storage.Rows, error) {
	return p.Transaction.(storage.QueryExecutor).Query(ctx, sql, args...)
}
func installWriteFault(t *testing.T, f *runtimeFixture, position storage.Position, fault func(context.Context, storage.Transaction) error) *writeFaultDatabase {
	t.Helper()
	c, err := f.wc.Databases.Business(position)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := &writeFaultDatabase{Postgres: f.wr.Database.(*adapters.Postgres), target: c, fault: fault}
	f.wr.Database = wrapper
	return wrapper
}
func coordinator(f *runtimeFixture) *coord.Coordinator {
	return &coord.Coordinator{Store: f.qr.Control, Proof: f.qr.Database.(*adapters.Postgres), Clock: realClock{}, Random: rand.Reader, ProcessID: f.qr.ProcessID}
}
func mainBaseline(t *testing.T) *runtimeFixture {
	t.Helper()
	f := newRuntime(t, true)
	f.run(t, "force_full", "100")
	a := activeMetadata(t, f)
	if a.Position.BundleID != "C100" || a.Position.Position != (storage.Position{Instance: storage.Main, Slot: 1}) {
		t.Fatal("M1/C100 baseline missing")
	}
	return f
}
func changeGradeNow(t *testing.T, f *runtimeFixture) string {
	t.Helper()
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Second).In(zone).Truncate(time.Microsecond)
	f.source.mu.Lock()
	defer f.source.mu.Unlock()
	var row map[string]json.RawMessage
	if err := json.Unmarshal(f.source.rows["dwd_bzkscjzlb"][1], &row); err != nil {
		t.Fatal(err)
	}
	row["KCCJ"] = json.RawMessage("91")
	row["ETL_TIME"], _ = json.Marshal(at.Format("2006-01-02 15:04:05.999999"))
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	f.source.rows["dwd_bzkscjzlb"][1] = raw
	return at.Format(time.RFC3339Nano)
}
func assertBeforeSecondCopy(t *testing.T, f *runtimeFixture, shifted bool) {
	t.Helper()
	a := activeMetadata(t, f)
	want := storage.Main
	if shifted {
		want = storage.Standby
	}
	if a.Position.BundleID != "C100" || a.Position.Position.Instance != want || a.Position.Position.Slot != 1 {
		t.Fatal("unsafe route after interrupted update")
	}
	if countControl(t, f, "SELECT count(*) FROM control.candidates WHERE task_id='101' AND instance='S' AND state='ready'") != 1 {
		t.Fatal("first new copy not ready")
	}
	if countControl(t, f, "SELECT count(*) FROM control.publication_outbox WHERE task_id='101' AND kind='bundle_publish'") != 0 || countControl(t, f, "SELECT count(*) FROM control.source_progress WHERE task_id='100'") != 14 {
		t.Fatal("partial round advanced publication/progress")
	}
	if !shifted && countControl(t, f, "SELECT count(*) FROM control.candidates WHERE task_id='101' AND instance='M' AND state<>'planned'") != 0 {
		t.Fatal("second instance build started before old-copy verification")
	}
}
func TestI04_04(t *testing.T) {
	f := mainBaseline(t)
	a := activeMetadata(t, f)
	h := read.Handle{Position: a.Position.Position, Generation: a.Position.Generation, Combination: a.Position.BundleID}
	lease, err := coordinator(f).AcquireFixed(f.e.ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	changeGradeNow(t, f)
	admitTask(t, f, "incremental", "101")
	if err := f.worker.Engine.Cycle(f.e.ctx); !errors.Is(err, update.ErrNotDrained) {
		t.Fatalf("expected outstanding-reader drain barrier: %v", err)
	}
	assertBeforeSecondCopy(t, f, true)
	if countControl(t, f, "SELECT count(*) FROM control.checkpoints WHERE task_id='101' AND name='readability-proof'") != 1 || countControl(t, f, "SELECT count(*) FROM control.publication_outbox WHERE task_id='101' AND kind='route_shift' AND status='confirmed'") != 1 || countControl(t, f, "SELECT count(*) FROM control.candidates WHERE task_id='101' AND instance='M' AND state='planned'") != 1 {
		t.Fatal("build/check/shift/drain order violated")
	}
	if err := lease.Close(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.worker.Engine.Cycle(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	if task := loadTask(t, f, "101"); task.Status != "success_published" {
		t.Fatal("drained update did not finish")
	}
	assertProgress(t, f, "101")
}
func oldCopyFailure(t *testing.T, kind string) {
	f := mainBaseline(t)
	changeGradeNow(t, f)
	old := f.e.connection(t, 3)
	controlDB := f.e.connection(t, 4)
	var restore func() error
	switch kind {
	case "data":
		restore = func() error {
			_, err := old.Exec(context.Background(), `UPDATE source.dwd_bzkscjzlb SET "KCCJ"='90' WHERE "ID"='G02'`)
			return err
		}
	case "permission":
		restore = func() error {
			_, err := old.Exec(context.Background(), "GRANT SELECT ON source.dwd_bzkscjzlb TO sicp_query")
			return err
		}
	case "evidence":
		var saved string
		if err := controlDB.QueryRow(f.e.ctx, "SELECT evidence::text FROM control.candidates WHERE id='100-S'").Scan(&saved); err != nil {
			t.Fatal(err)
		}
		restore = func() error {
			_, err := controlDB.Exec(context.Background(), "UPDATE control.candidates SET evidence=$1::jsonb WHERE id='100-S'", saved)
			return err
		}
	}
	t.Cleanup(func() {
		if err := restore(); err != nil {
			t.Error(err)
		}
	})
	fault := installWriteFault(t, f, storage.Position{Instance: storage.Standby, Slot: 0}, func(ctx context.Context, _ storage.Transaction) error {
		switch kind {
		case "data":
			_, err := old.Exec(ctx, `UPDATE source.dwd_bzkscjzlb SET "KCCJ"='-999' WHERE "ID"='G02'`)
			return err
		case "permission":
			_, err := old.Exec(ctx, "REVOKE SELECT ON source.dwd_bzkscjzlb FROM sicp_query")
			return err
		case "evidence":
			_, err := controlDB.Exec(ctx, `UPDATE control.candidates SET evidence=jsonb_set(evidence,'{Combination}','"wrong-bundle"') WHERE id='100-S'`)
			return err
		}
		return errors.New("unknown fault")
	})
	admitTask(t, f, "incremental", "101")
	if err := f.worker.Engine.Cycle(f.e.ctx); err == nil || !fault.triggered {
		t.Fatal("old copy fault did not block update", err)
	}
	assertBeforeSecondCopy(t, f, false)
	data, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
	if items := data["items"].([]any); len(items) != 2 || !reflect.DeepEqual(items[1], expectedJSON(t, "grade_g02.json")) {
		t.Fatal("healthy M/C100 stopped serving old result")
	}
}
func TestI04_05(t *testing.T) { oldCopyFailure(t, "data") }
func TestI04_06(t *testing.T) {
	for _, kind := range []string{"permission", "evidence"} {
		t.Run(kind, func(t *testing.T) { oldCopyFailure(t, kind) })
	}
}
func failSecondCopy(t *testing.T, f *runtimeFixture) *writeFaultDatabase {
	t.Helper()
	fault := installWriteFault(t, f, storage.Position{Instance: storage.Main, Slot: 0}, func(ctx context.Context, tx storage.Transaction) error {
		_, err := tx.Exec(ctx, "SELECT * FROM source.synthetic_missing_table")
		return err
	})
	admitTask(t, f, "incremental", "101")
	if err := f.worker.Engine.Cycle(f.e.ctx); err == nil || !fault.triggered {
		t.Fatal("second-copy fault did not interrupt update", err)
	}
	assertBeforeSecondCopy(t, f, true)
	if countControl(t, f, "SELECT count(*) FROM control.candidates WHERE task_id='101' AND instance='M' AND state='failed'") != 1 {
		t.Fatal("failed second copy remained readable")
	}
	data, _ := f.queryOK(t, contract.StudentGrades, allGrades, "")
	if items := data["items"].([]any); len(items) != 2 || !reflect.DeepEqual(items[1], expectedJSON(t, "grade_g02.json")) {
		t.Fatal("S/C100 did not retain old result")
	}
	return fault
}
func TestI04_07(t *testing.T) {
	f := mainBaseline(t)
	changeGradeNow(t, f)
	failSecondCopy(t, f)
}
func TestI04_08(t *testing.T) {
	f := mainBaseline(t)
	stamp := changeGradeNow(t, f)
	fault := failSecondCopy(t, f)
	f.wr.Database = fault.Postgres
	f.source.mu.Lock()
	before := f.source.requests
	f.source.mu.Unlock()
	if err := f.worker.Engine.Cycle(f.e.ctx); err != nil {
		t.Fatal("local recovery", err)
	}
	f.source.mu.Lock()
	after := f.source.requests
	f.source.mu.Unlock()
	if before != after {
		t.Fatal("recovery reacquired source instead of using sealed input")
	}
	a := activeMetadata(t, f)
	if a.Position.BundleID != "C101" || a.Position.Position != (storage.Position{Instance: storage.Standby, Slot: 0}) {
		t.Fatal("recovered publication incorrect")
	}
	if countControl(t, f, "SELECT count(*) FROM control.candidates WHERE task_id='101' AND state='ready'") != 2 {
		t.Fatal("recovery published one copy")
	}
	assertProgress(t, f, "101")
	assertCopyResults(t, f, "C101", stamp)
}

func assertCopyResults(t *testing.T, f *runtimeFixture, combination, stamp string) {
	t.Helper()
	// Hash actual rows independently in PostgreSQL, not the stored manifest.
	left, right := f.e.connection(t, 0), f.e.connection(t, 2)
	for _, table := range source.Catalog() {
		sql := fmt.Sprintf(`SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY t."%s")::text,'[]')) FROM source.%s t`, table.PrimaryKey, table.Name)
		var a, b string
		if err := left.QueryRow(f.e.ctx, sql).Scan(&a); err != nil {
			t.Fatal(err)
		}
		if err := right.QueryRow(f.e.ctx, sql).Scan(&b); err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatalf("physical table digest differs: %s", table.Name)
		}
	}
	c := coordinator(f)
	reader := read.Repository{Storage: source.Repository{Registry: f.qc.Databases, Database: f.qr.Database}, Gate: c}
	times, err := compute.NewTimes("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	g := grades.Service{Source: reader, Times: times, Clock: realClock{}, Limits: read.DefaultLimits()}
	ts := timetable.Service{Source: reader, Times: times, Clock: realClock{}, Limits: read.DefaultLimits()}
	synced := realClock{}.Now()
	var results [2][]any
	for n, instance := range []storage.Instance{storage.Main, storage.Standby} {
		var generation int64
		if err := f.e.connection(t, 4).QueryRow(f.e.ctx, "SELECT generation FROM control.slots WHERE instance=$1 AND slot=0 AND bundle_id=$2 AND admission_open", string(instance), combination).Scan(&generation); err != nil {
			t.Fatal(err)
		}
		h := read.Handle{Position: storage.Position{Instance: instance, Slot: 0}, Generation: generation, Combination: combination}
		for _, op := range []contract.Operation{contract.StudentGrades, contract.BatchGrades, contract.StudentTimetable, contract.StaffTimetable} {
			result, err := read.ExecuteFixed(f.e.ctx, c, h, func(ctx context.Context, h read.Handle) (any, error) {
				switch op {
				case contract.StudentGrades:
					var req contract.StudentGradesRequest
					if err := json.Unmarshal([]byte(allGrades), &req); err != nil {
						return nil, err
					}
					return g.Student(ctx, h, req, synced)
				case contract.BatchGrades:
					var req contract.BatchGradesRequest
					if err := json.Unmarshal([]byte(`{"student_numbers":["001"],"academic_year":"2026-2027","page":1,"page_size":100}`), &req); err != nil {
						return nil, err
					}
					return g.Batch(ctx, h, req, synced)
				case contract.StudentTimetable:
					var req contract.StudentTimetableRequest
					if err := json.Unmarshal([]byte(`{"student_number":"001","academic_year":"2026-2027","semester":"1"}`), &req); err != nil {
						return nil, err
					}
					return ts.Student(ctx, h, req, synced)
				default:
					var req contract.StaffTimetableRequest
					if err := json.Unmarshal([]byte(`{"staff_number":"001","academic_year":"2026-2027","semester":"1"}`), &req); err != nil {
						return nil, err
					}
					return ts.Staff(ctx, h, req, synced)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			items := decoded["items"].([]any)
			if op == contract.StudentGrades || op == contract.BatchGrades {
				want := expectedJSON(t, "grade_g02.json").(map[string]any)
				want["score"], want["source_updated_at"] = float64(91), stamp
				if decoded["total"] != float64(2) || len(items) != 2 || !reflect.DeepEqual(items[1], want) {
					t.Fatal("recovered grade differs from independent expected")
				}
			} else if !reflect.DeepEqual(decoded["items"], expectedJSON(t, "timetable_items.json")) {
				t.Fatal("recovered timetable differs from independent expected")
			}
			results[n] = append(results[n], decoded)
		}
	}
	if !reflect.DeepEqual(results[0], results[1]) {
		t.Fatal("four query results differ between physical copies")
	}
}
