package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/config"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/coord"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/handlers"
	querymeta "github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/service"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type pipelineRandom struct {
	mu sync.Mutex
	n  byte
}

func (r *pipelineRandom) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	for i := range p {
		p[i] = r.n
	}
	return len(p), nil
}

// This world replaces external I/O only. Real transport, snapshot manager,
// metadata provider, coalescer, coordinator, read gate and business services run.
type pipelineWorld struct {
	mu                     sync.Mutex
	active                 read.Handle
	readable               map[read.Handle]bool
	pins                   map[string]control.Snapshot
	cache                  map[string][]byte
	readers                map[string]control.SlotIdentity
	executions             map[string]string
	stopped                map[string]bool
	closed                 map[string]bool
	sql                    []read.Handle
	trace                  []string
	checks                 time.Time
	getErr, setErr, sqlErr error
	observed               int
	block                  chan struct{}
	started                chan struct{}
	blockOnce              sync.Once
	logs                   []events.Event
}

func newWorld() *pipelineWorld {
	return &pipelineWorld{active: pinned, readable: map[read.Handle]bool{pinned: true}, pins: map[string]control.Snapshot{}, cache: map[string][]byte{}, readers: map[string]control.SlotIdentity{}, executions: map[string]string{}, stopped: map[string]bool{}, closed: map[string]bool{}, checks: time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC)}
}
func (w *pipelineWorld) Active(context.Context) (read.Handle, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trace = append(w.trace, "active")
	return w.active, nil
}
func (w *pipelineWorld) ForCombination(_ context.Context, bundle string) (read.Handle, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for h, yes := range w.readable {
		if yes && h.Combination == bundle {
			return h, nil
		}
	}
	return read.Handle{}, read.ErrHandle
}
func (w *pipelineWorld) validate(_ context.Context, h read.Handle) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trace = append(w.trace, "validate")
	if !w.readable[h] {
		return read.ErrHandle
	}
	return nil
}
func (w *pipelineWorld) PutSnapshot(_ context.Context, s control.Snapshot) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.pins[s.Token]; ok {
		return errors.New("duplicate token")
	}
	w.pins[s.Token] = s
	return nil
}
func (w *pipelineWorld) GetSnapshot(_ context.Context, id string) (control.Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.pins[id]
	if !ok {
		return s, control.ErrMissing
	}
	return s, nil
}
func (w *pipelineWorld) Get(_ context.Context, key string) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trace = append(w.trace, "get")
	return append([]byte(nil), w.cache[key]...), w.getErr
}
func (w *pipelineWorld) Set(_ context.Context, key string, value []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trace = append(w.trace, "set")
	if w.setErr != nil {
		return w.setErr
	}
	w.cache[key] = append([]byte(nil), value...)
	return nil
}
func (w *pipelineWorld) BundleChecks(_ context.Context, bundle string) (map[string]time.Time, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trace = append(w.trace, "metadata:"+bundle)
	out := map[string]time.Time{}
	for _, def := range contract.Definitions() {
		deps, _ := querymeta.Dependencies(def.Operation)
		for _, table := range deps {
			out[table] = w.checks
		}
	}
	return out, nil
}
func (w *pipelineWorld) RegisterReader(_ context.Context, h control.SlotIdentity, id, _ string, _ time.Time) (control.Reader, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	actual := read.Handle{Position: h.Position, Generation: h.Generation, Combination: h.BundleID}
	if !w.readable[actual] {
		return control.Reader{}, read.ErrHandle
	}
	w.trace = append(w.trace, "register")
	w.readers[id] = h
	return control.Reader{ID: id}, nil
}
func (w *pipelineWorld) ReleaseReader(_ context.Context, _ control.SlotIdentity, id string, _ time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trace = append(w.trace, "release")
	w.closed[id] = true
	return nil
}
func (w *pipelineWorld) BeginSQL(_ context.Context, h control.SlotIdentity, reader, id string, _ time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed[reader] || w.readers[reader] != h {
		return read.ErrHandle
	}
	w.trace = append(w.trace, "beginSQL")
	w.executions[id] = reader
	return nil
}
func (w *pipelineWorld) FinishSQL(_ context.Context, _ control.SlotIdentity, _, id string, _ time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped[id] {
		return errors.New("running SQL")
	}
	w.trace = append(w.trace, "finishSQL")
	return nil
}
func (w *pipelineWorld) Stopped(_ context.Context, id string) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stopped[id], nil
}
func (w *pipelineWorld) ReadBatch(context.Context, storage.Position, string, string, int) ([]source.SourceRow, error) {
	return nil, errors.New("unexpected unfiltered scan")
}
func (w *pipelineWorld) ReadMatching(ctx context.Context, p storage.Position, table, _ string, _ int, _ []source.Filter) ([]source.SourceRow, error) {
	w.mu.Lock()
	id := coord.ExecutionID(ctx)
	reader, ok := w.executions[id]
	h := w.readers[reader]
	if !ok || h.Position != p || w.closed[reader] {
		w.mu.Unlock()
		return nil, errors.New("SQL without registered fixed lease")
	}
	w.trace = append(w.trace, "sql")
	w.sql = append(w.sql, read.Handle{Position: p, Generation: h.Generation, Combination: h.BundleID})
	block, started, err := w.block, w.started, w.sqlErr
	w.mu.Unlock()
	if block != nil {
		w.blockOnce.Do(func() { close(started); <-block })
	}
	w.mu.Lock()
	w.stopped[id] = true
	w.mu.Unlock()
	return []source.SourceRow{}, err
}
func pipelineSetup(t *testing.T, w *pipelineWorld) (http.Handler, *cache.Coalescer) {
	t.Helper()
	clock := testkit.FakeClock{Time: time.Date(2026, 9, 18, 2, 0, 0, 0, time.UTC)}
	random := &pipelineRandom{}
	recorder, err := events.New(clock, random, events.SinkFunc(func(_ context.Context, e events.Event) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.logs = append(w.logs, e)
		return nil
	}), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := &coord.Coordinator{Store: w, Proof: w, Clock: clock, Random: random, ProcessID: "synthetic-query"}
	src := read.Repository{Storage: w, Gate: c}
	times, _ := compute.NewTimes("Asia/Shanghai")
	flights, err := cache.NewCoalescer(2, 4, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := flights.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	p := handlers.Pipeline{Grades: grades.Service{Source: src, Times: times, Clock: clock, Limits: read.DefaultLimits()}, Timetable: timetable.Service{Source: src, Times: times, Clock: clock, Limits: read.DefaultLimits()}, Admission: c, Snapshots: snapshot.Manager{Store: w, Routes: w, Clock: clock, Random: random}, Cache: cache.Executor{Results: cache.Results{Store: w, Observe: func(error) { w.mu.Lock(); w.observed++; w.mu.Unlock() }}, Flights: flights, Validate: w.validate}, Metadata: querymeta.Provider{Store: w}, Times: times}
	registered, err := p.Handlers("synthetic-key", recorder, func(*events.Span, error) { t.Error("log failure") })
	if err != nil {
		t.Fatal(err)
	}
	h, err := service.NewHTTPWithHandlers(config.Query, "synthetic-key", clock, random, testkit.ReadyFunc(func(context.Context) bool { return true }), registered)
	if err != nil {
		t.Fatal(err)
	}
	return h, flights
}
func pipelineRequest(ctx context.Context, h http.Handler, op contract.Operation, body, key, token string) *httptest.ResponseRecorder {
	path := ""
	for _, d := range contract.Definitions() {
		if d.Operation == op {
			path = d.Path
		}
	}
	r := httptest.NewRequest("POST", path, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("X-API-Key", key)
	if token != "" {
		r.Header.Set(snapshot.Header, token)
	}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	return out
}

func TestT11_04AllFourRealPipelinesColdHotFreshMetadataAndAuthenticatedHits(t *testing.T) {
	for _, tc := range []struct {
		op   contract.Operation
		body string
	}{
		{contract.StudentGrades, `{"student_number":"001"}`},
		{contract.BatchGrades, `{"academic_year":"2025-2026"}`},
		{contract.StudentTimetable, studentTimetableBody},
		{contract.StaffTimetable, `{"staff_number":"T001","academic_year":"2026-2027","semester":"1"}`},
	} {
		t.Run(string(tc.op), func(t *testing.T) {
			w := newWorld()
			h, _ := pipelineSetup(t, w)
			cold := pipelineRequest(t.Context(), h, tc.op, tc.body, "synthetic-key", "")
			if cold.Code != 200 || !strings.Contains(cold.Body.String(), `"items":[]`) {
				t.Fatal(cold.Code, cold.Body.String())
			}
			w.mu.Lock()
			sqlCount := len(w.sql)
			traceCount := len(w.trace)
			w.checks = w.checks.Add(time.Hour)
			w.mu.Unlock()
			bad := pipelineRequest(t.Context(), h, tc.op, tc.body, "invalid", cold.Header().Get(snapshot.Header))
			w.mu.Lock()
			unchanged := len(w.trace) == traceCount
			w.mu.Unlock()
			if bad.Code != 401 || !unchanged {
				t.Fatal("cache bypassed auth", bad.Code)
			}
			hot := pipelineRequest(t.Context(), h, tc.op, tc.body, "synthetic-key", cold.Header().Get(snapshot.Header))
			if hot.Code != 200 || !strings.Contains(hot.Body.String(), `"synced_at":"2026-09-18T10:00:00+08:00"`) || hot.Header().Get("X-Request-ID") == cold.Header().Get("X-Request-ID") {
				t.Fatal(hot.Code, hot.Body.String())
			}
			w.mu.Lock()
			defer w.mu.Unlock()
			if sqlCount == 0 || len(w.sql) != sqlCount || len(w.readers) != 1 || len(w.closed) != 1 || len(w.cache) != 1 {
				t.Fatal(w.trace, w.sql, w.readers)
			}
			for _, payload := range w.cache {
				if strings.Contains(string(payload), "synced_at") || strings.Contains(string(payload), "request_id") {
					t.Fatal(string(payload))
				}
			}
			for i, item := range w.trace {
				if item == "sql" && (i == 0 || w.trace[i-1] != "beginSQL") {
					t.Fatal(w.trace)
				}
			}
		})
	}
}

func TestT11_14RealSnapshotMoveReusesCacheAndChangedConditionsNeverReadSQL(t *testing.T) {
	w := newWorld()
	h, _ := pipelineSetup(t, w)
	body := `{"student_number":"001","page_size":1}`
	first := pipelineRequest(t.Context(), h, contract.StudentGrades, body, "synthetic-key", "")
	token := first.Header().Get(snapshot.Header)
	if first.Code != 200 || len(token) != 64 {
		t.Fatal(first.Code, first.Body.String())
	}
	w.mu.Lock()
	moved := pinned
	moved.Position = storage.Position{Instance: storage.Main, Slot: 0}
	moved.Generation++
	w.readable[pinned] = false
	w.readable[moved] = true
	w.active = moved
	count := len(w.sql)
	w.mu.Unlock()
	same := pipelineRequest(t.Context(), h, contract.StudentGrades, body, "synthetic-key", token)
	if same.Code != 200 || same.Header().Get(snapshot.Header) != token {
		t.Fatal(same.Code, same.Body.String())
	}
	w.mu.Lock()
	if len(w.sql) != count {
		t.Error("same combination missed cache")
	}
	w.mu.Unlock()
	page2 := pipelineRequest(t.Context(), h, contract.StudentGrades, `{"student_number":"001","page_size":1,"page":2}`, "synthetic-key", token)
	if page2.Code != 200 {
		t.Fatal(page2.Code, page2.Body.String())
	}
	changed := pipelineRequest(t.Context(), h, contract.StudentGrades, `{"student_number":"002","page_size":1}`, "synthetic-key", token)
	if changed.Code != 409 || changed.Header().Get(snapshot.Header) != "" {
		t.Fatal(changed.Code, changed.Body.String())
	}
	w.mu.Lock()
	w.readable[moved] = false
	w.active.Combination = "new-C"
	w.readable[w.active] = true
	w.mu.Unlock()
	expired := pipelineRequest(t.Context(), h, contract.StudentGrades, body, "synthetic-key", token)
	if expired.Code != 409 {
		t.Fatal("fell back to new combination", expired.Code)
	}
}

func waitPipeline(t *testing.T, f *cache.Coalescer, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		_, waiting, _ := f.State()
		if waiting == n {
			return
		}
		if ctx.Err() != nil {
			t.Fatal("waiters", waiting)
		}
		runtime.Gosched()
	}
}

func pipelineReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline channel did not complete")
		var zero T
		return zero
	}
}
func TestT11_07RealCanceledLeaderKeepsIndependentReaderForOtherWaiter(t *testing.T) {
	w := newWorld()
	w.block = make(chan struct{})
	w.started = make(chan struct{})
	h, f := pipelineSetup(t, w)
	var unblock sync.Once
	defer unblock.Do(func() { close(w.block) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := `{"student_number":"001"}`
	first := make(chan *httptest.ResponseRecorder, 1)
	second := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- pipelineRequest(ctx, h, contract.StudentGrades, body, "synthetic-key", "") }()
	pipelineReceive(t, w.started)
	go func() { second <- pipelineRequest(t.Context(), h, contract.StudentGrades, body, "synthetic-key", "") }()
	waitPipeline(t, f, 2)
	cancel()
	if got := pipelineReceive(t, first); got.Code != 504 {
		t.Fatal(got.Code, got.Body.String())
	}
	w.mu.Lock()
	closed := len(w.closed)
	w.mu.Unlock()
	if closed != 0 {
		t.Fatal("request cancellation released worker reader")
	}
	unblock.Do(func() { close(w.block) })
	if got := pipelineReceive(t, second); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.sql) != 1 || len(w.readers) != 1 || len(w.closed) != 1 {
		t.Fatal(w.trace)
	}
}

func TestT11_15RealLateFillRemainsUnderOriginalCombination(t *testing.T) {
	w := newWorld()
	release := make(chan struct{})
	w.block = release
	w.started = make(chan struct{})
	h, _ := pipelineSetup(t, w)
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	body := `{"student_number":"001"}`
	old := make(chan *httptest.ResponseRecorder, 1)
	go func() { old <- pipelineRequest(t.Context(), h, contract.StudentGrades, body, "synthetic-key", "") }()
	pipelineReceive(t, w.started)
	w.mu.Lock()
	next := pinned
	next.Combination = "new-C"
	next.Generation++
	next.Position.Slot = 0
	w.active = next
	w.readable[next] = true
	w.block = nil
	w.mu.Unlock()
	newResult := pipelineRequest(t.Context(), h, contract.StudentGrades, body, "synthetic-key", "")
	if newResult.Code != 200 {
		t.Fatal(newResult.Code, newResult.Body.String())
	}
	unblock.Do(func() { close(release) })
	if got := pipelineReceive(t, old); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.cache) != 2 || len(w.sql) != 2 || w.sql[0] != pinned || w.sql[1] != next {
		t.Fatal(w.sql, w.cache)
	}
	oldKey, newKey := false, false
	for key := range w.cache {
		oldKey = oldKey || strings.HasPrefix(key, "query:C09:")
		newKey = newKey || strings.HasPrefix(key, "query:new-C:")
	}
	if !oldKey || !newKey {
		t.Fatal(w.cache)
	}
}

func TestT11_08RealPipelineCacheFailureAndUnconfirmedRouteBoundaries(t *testing.T) {
	for _, kind := range []string{"get", "set", "unconfirmed", "sql", "capacity"} {
		t.Run(kind, func(t *testing.T) {
			w := newWorld()
			h, f := pipelineSetup(t, w)
			want := 200
			switch kind {
			case "get":
				w.getErr = errors.New("cache down")
			case "set":
				w.setErr = errors.New("cache write failed")
			case "unconfirmed":
				w.readable[pinned] = false
				want = 503
			case "sql":
				w.sqlErr = errors.New("private SQL error")
				want = 503
			case "capacity":
				if err := f.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
				want = 422
			}
			got := pipelineRequest(t.Context(), h, contract.StudentGrades, `{"student_number":"001"}`, "synthetic-key", "")
			if got.Code != want || strings.Contains(got.Body.String(), "private") {
				t.Fatal(got.Code, got.Body.String())
			}
			w.mu.Lock()
			defer w.mu.Unlock()
			if kind == "unconfirmed" && (len(w.sql) != 0 || strings.Contains(strings.Join(w.trace, ","), "get")) {
				t.Fatal(w.trace)
			}
			if (kind == "get" || kind == "set") && (w.observed == 0 || len(w.sql) != 1) {
				t.Fatal(w.observed, w.trace)
			}
			if kind == "sql" && len(w.cache) != 0 {
				t.Fatal("failure cached")
			}
		})
	}
}

func TestT11_16RealSnapshotHeadersAreStrictAndAuthRunsFirst(t *testing.T) {
	w := newWorld()
	h, _ := pipelineSetup(t, w)
	for _, tc := range []struct {
		op        contract.Operation
		body, key string
		values    []string
		status    int
	}{
		{contract.StudentGrades, `{"student_number":"001"}`, "synthetic-key", []string{""}, 409},
		{contract.StudentGrades, `{"student_number":"001"}`, "synthetic-key", []string{strings.Repeat("a", 64), strings.Repeat("a", 64)}, 409},
		{contract.StudentGrades, `{"student_number":"001"}`, "wrong", []string{"bad"}, 401},
		{contract.StudentTimetable, studentTimetableBody, "synthetic-key", []string{strings.Repeat("a", 64)}, 400},
	} {
		path := ""
		for _, d := range contract.Definitions() {
			if d.Operation == tc.op {
				path = d.Path
			}
		}
		r := httptest.NewRequest("POST", path, strings.NewReader(tc.body))
		r.Header.Set("X-API-Key", tc.key)
		r.Header[snapshot.Header] = tc.values
		got := httptest.NewRecorder()
		h.ServeHTTP(got, r)
		if got.Code != tc.status || got.Header().Get(snapshot.Header) != "" {
			t.Fatal(got.Code, got.Body.String())
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.sql) != 0 || len(w.trace) != 0 {
		t.Fatal(w.trace)
	}
}
