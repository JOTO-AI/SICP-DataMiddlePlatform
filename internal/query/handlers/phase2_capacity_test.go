package handlers_test

import (
	"context"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
)

func TestT24_07TeacherPrefilterStillLoadsAllCoteachers(t *testing.T) {
	repo := teachingClassesRepo()
	for i := 0; i < 100; i++ {
		row := sourceRow(`{"PKH":"unrelated","JXBH":"other","JHKCH":"C2","KKXND":"2026-2027","KKXQM":"1","JSJGH":"unrelated"}`)
		// Duplicate unrelated rows deliberately exceed the source budget if
		// the candidate scan ever loses its teacher prefilter.
		repo.tables[timetable.SchedulesTable] = append(repo.tables[timetable.SchedulesTable], row)
	}
	svc, _ := teachingService(t, repo)
	limits := read.DefaultLimits()
	limits.Rows = 20
	b, _ := read.NewBudget(limits, svc.Clock)
	got, err := svc.StaffClasses(t.Context(), pinned, teacherClassesRequest(), b)
	if err != nil || len(got) != 1 || len(got[0].Teachers) != 3 || got[0].StudentCount != 2 {
		t.Fatal(got, err)
	}
}

func TestT24_08FivePipelinesRejectExcessWaiters(t *testing.T) {
	for _, tc := range phase2PipelineCases() {
		t.Run(string(tc.op), func(t *testing.T) {
			w := newWorld()
			w.tables = tc.repo.tables
			w.block = make(chan struct{})
			w.started = make(chan struct{})
			h, flights := pipelineSetup(t, w)
			released := false
			defer func() {
				if !released {
					close(w.block)
				}
			}()
			results := make(chan *httptest.ResponseRecorder, 6)
			for i := 0; i < 6; i++ {
				go func() { results <- pipelineRequest(t.Context(), h, tc.op, tc.body, "synthetic-key", "") }()
			}
			select {
			case <-w.started:
			case <-time.After(5 * time.Second):
				t.Fatal("SQL never started")
			}
			waitPipeline(t, flights, 6)
			excess := pipelineRequest(t.Context(), h, tc.op, tc.body, "synthetic-key", "")
			if excess.Code != 422 {
				t.Fatal(excess.Code, excess.Body.String())
			}
			w.mu.Lock()
			if len(w.sql) != 1 || len(w.readers) != 1 {
				t.Error("coalesced waiters opened extra SQL", len(w.sql), len(w.readers))
			}
			w.mu.Unlock()
			close(w.block)
			released = true
			for i := 0; i < 6; i++ {
				got := pipelineReceive(t, results)
				if got.Code != 200 {
					t.Fatal(got.Code, got.Body.String())
				}
			}
		})
	}
}

func TestT24_09FiveCancellationsWaitForActualSQLCleanup(t *testing.T) {
	for _, tc := range phase2PipelineCases() {
		t.Run(string(tc.op), func(t *testing.T) {
			w := newWorld()
			w.tables = tc.repo.tables
			w.block = make(chan struct{})
			w.started = make(chan struct{})
			h, flights := pipelineSetup(t, w)
			released := false
			defer func() {
				if !released {
					close(w.block)
				}
			}()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() { result <- pipelineRequest(ctx, h, tc.op, tc.body, "synthetic-key", "") }()
			select {
			case <-w.started:
			case <-time.After(5 * time.Second):
				t.Fatal("SQL never started")
			}
			cancel()
			got := pipelineReceive(t, result)
			if got.Code != 504 {
				t.Fatal(got.Code, got.Body.String())
			}
			w.mu.Lock()
			if len(w.closed) != 0 || len(w.cache) != 0 {
				t.Error("running SQL released or failed response cached")
			}
			w.mu.Unlock()
			close(w.block)
			released = true
			deadline := time.Now().Add(5 * time.Second)
			for {
				running, waiting, active := flights.State()
				if running == 0 && waiting == 0 && active == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("SQL cleanup did not finish")
				}
				runtime.Gosched()
			}
			w.mu.Lock()
			defer w.mu.Unlock()
			if len(w.closed) != 1 || len(w.cache) != 0 {
				t.Fatal(w.trace)
			}
			for id := range w.executions {
				if !w.stopped[id] {
					t.Fatal("SQL completion not proved")
				}
			}
		})
	}
}
