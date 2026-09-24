package update_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type timer struct{ now time.Time }

func (c *timer) Now() time.Time { return c.now }
func (c *timer) Wait(ctx context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	return ctx.Err()
}

type cooldown struct{ until time.Time }

func (c *cooldown) Cooldown(context.Context) (time.Time, error)          { return c.until, nil }
func (c *cooldown) ExtendCooldown(_ context.Context, at time.Time) error { c.until = at; return nil }

type admission struct{}

func (admission) Acquire(context.Context) (func(), error) { return func() {}, nil }

type roundTrip func(*http.Request) (*http.Response, error)

func (r roundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return r(request) }

type initialWorld struct {
	t      *testing.T
	trace  []string
	fail   string
	input  candidate.Manifest
	intent control.PublicationIntent
	builds int
	plan   control.ProgressPlan
}

func (w *initialWorld) record(step string) error {
	w.trace = append(w.trace, step)
	if w.fail == step {
		return errors.New("synthetic " + step)
	}
	return nil
}
func (w *initialWorld) PlanInitial(context.Context, update.InitialTask) ([2]source.Target, error) {
	return [2]source.Target{{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 1, CandidateID: "m"}, {Position: storage.Position{Instance: storage.Standby, Slot: 0}, Generation: 1, CandidateID: "s"}}, w.record("plan")
}
func (w *initialWorld) RecordTarget(_ context.Context, _ update.InitialTask, _ [2]source.Target, m candidate.Manifest) error {
	w.input = m
	return w.record("target")
}
func (w *initialWorld) Build(_ context.Context, target source.Target, m candidate.Manifest, force bool) (candidate.Report, error) {
	w.builds++
	if !force || !reflect.DeepEqual(m, w.input) {
		w.t.Fatal("copies did not receive identical complete target")
	}
	err := w.record("build-" + target.CandidateID)
	return candidate.Report{StorageComplete: err == nil}, err
}
func (w *initialWorld) Validate(_ context.Context, target source.Target, m candidate.Manifest) (candidate.Evidence, error) {
	if !reflect.DeepEqual(m, w.input) {
		w.t.Fatal("validation changed target")
	}
	if err := w.record("ready-" + target.CandidateID); err != nil {
		return candidate.Evidence{}, err
	}
	e := candidate.Evidence{Target: target, Combination: m.Combination, CodeVersion: "v1", CheckedAt: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), Queries: map[contract.Operation]candidate.QueryEvidence{}}
	for _, definition := range contract.Definitions() {
		tables, err := metadata.Dependencies(definition.Operation)
		if err != nil {
			w.t.Fatal(err)
		}
		deps := map[string]string{}
		for _, name := range tables {
			for _, v := range m.Tables {
				if v.Table == name {
					deps[name] = v.Scope + ":" + v.SHA256
				}
			}
		}
		e.Queries[definition.Operation] = candidate.QueryEvidence{Dependencies: deps, Cases: 1, SHA256: strings.Repeat("a", 64)}
	}
	if w.fail == "stale-evidence" {
		e.Target.Generation++
	}
	return e, nil
}
func (w *initialWorld) EnqueuePublication(_ context.Context, intent control.PublicationIntent) (control.Publication, bool, error) {
	w.intent = intent
	return control.Publication{ID: intent.ID, Status: "pending"}, true, w.record("enqueue")
}
func (w *initialWorld) Apply(context.Context, control.Publication) error { return w.record("apply") }

func (w *initialWorld) StageProgress(_ context.Context, p control.ProgressPlan) error {
	w.plan = p
	return w.record("progress-plan")
}
func (w *initialWorld) ConfirmPublication(_ context.Context, e control.Publication, _ time.Time) (control.Publication, error) {
	if err := w.record("confirm"); err != nil {
		return control.Publication{}, err
	}
	e.Status = "confirmed"
	return e, nil
}
func (w *initialWorld) CompletePublished(context.Context, control.Publication, time.Time) error {
	return w.record("progress")
}

func initialization(t *testing.T, failure string) (update.Initializer, update.InitialTask, *initialWorld, *[]string) {
	t.Helper()
	w := &initialWorld{t: t, fail: failure}
	dir, err := staging.OpenDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	quota, err := staging.NewQuotaFiles(dir, 32<<20)
	if err != nil {
		t.Fatal(err)
	}
	store, err := staging.NewStore("full-round", quota, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	clock := &timer{now: time.Date(2100, 1, 1, 8, 0, 0, 0, time.UTC)}
	var requests []string
	retried := false
	client, err := ingest.NewClient("https://mock.invalid", "synthetic", roundTrip(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if q.Has("startTime") || q.Has("endTime") || len(q) != 3 {
			t.Fatal("full acquisition sent a time window")
		}
		name := path.Base(r.URL.Path)
		requests = append(requests, name+":"+q.Get("page"))
		status := 200
		body := `{"code":200,"message":"ok","data":{"total":0,"list":[]}}`
		if name == "dwd_xqjbsjzlb" {
			if !retried {
				retried = true
				status = 503
			} else if q.Get("page") == "1" {
				body = `{"code":200,"message":"ok","data":{"total":3,"list":[{"XQH":"a"},{"XQH":"b"}]}}`
			} else {
				body = `{"code":200,"message":"ok","data":{"total":3,"list":[{"XQH":"c"}]}}`
			}
		}
		if failure == "source" && name == ingest.Catalog()[len(ingest.Catalog())-1].Name {
			status = 403
		}
		if failure == "unknown" {
			body = `{"code":200,"message":"ok","data":{"total":1,"list":[{"UNEXPECTED_SYNTHETIC":"private"}]}}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := ingest.NewCoordinator(client, ingest.BudgetConfig{RequestTimeout: 10 * time.Second, PageTimeout: 30 * time.Second, InitialBackoff: time.Second, MaxBackoff: 2 * time.Second, MaxAttempts: 3, RetryHTTP: []int{503}}, clock, testkit.FixedRandom(0), &cooldown{}, admission{})
	if err != nil {
		t.Fatal(err)
	}
	budget, err := ingest.NewTaskBudget(clock.Now().Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	a := update.Acquirer{Collector: collector, Staging: store, Budget: budget, Limits: ingest.PageLimits{MaxPages: 100, MaxRecords: 1000, MaxBytes: 1 << 20}, PageSize: 2, Owner: "initialize", Observe: func(err error) { t.Errorf("unexpected observation: %v", err) }}
	i := update.Initializer{Store: w, Source: a, Preparer: candidate.TargetFiles{Files: quota, MaxChunkBytes: 512, MaxChunks: 1000}, Options: candidate.PrepareOptions{Scope: "mock-school", SpoolDirectory: t.TempDir(), Limits: merge.Limits{BatchRows: 2, BatchBytes: 256, FanIn: 2, MaxRuns: 1000, MaxSpoolBytes: 16 << 20, InputPageRows: 2, InputPageBytes: 1 << 20}}, Builder: w, Validator: w, Outbox: w, Delivery: w, Clock: clock, Progress: w}
	return i, update.InitialTask{ID: "init", Combination: "C1", CodeVersion: "v1", StartedAt: clock.Now()}, w, &requests
}

func TestT15_10AcquisitionPersistsFinalUnknownFieldNotification(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(fmt.Sprint(fails), func(t *testing.T) {
			i, _, _, _ := initialization(t, "unknown")
			a := i.Source.(update.Acquirer)
			calls := 0
			injected := errors.New("notification commit failed")
			a.Notify = func(ctx context.Context, f *ingest.FailureEvent) error {
				calls++
				if f == nil || ctx.Err() != nil {
					t.Fatal("missing final failure or independent context")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded notification persistence")
				}
				if fails {
					return injected
				}
				return nil
			}
			lease, err := a.Full(t.Context())
			if err == nil || lease != nil || calls != 1 || errors.Is(err, injected) != fails {
				t.Fatal(lease, err, calls)
			}
		})
	}
}

func TestT12_10FirstPublicationRequiresSameSealedTargetAndBothReadySerially(t *testing.T) {
	i, task, w, requests := initialization(t, "")
	result, err := i.Run(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"plan", "target", "build-m", "ready-m", "build-s", "ready-s", "progress-plan", "enqueue", "apply", "confirm", "progress"}
	if !reflect.DeepEqual(w.trace, want) || len(result.Manifest.Tables) != 14 {
		t.Fatalf("wrong serial initialization: %v", w.trace)
	}
	if w.intent.Expected != nil || w.intent.Kind != "bundle_publish" || w.intent.Target.Position.Instance != storage.Standby {
		t.Fatal("initialization attempted old-version route shift")
	}
	if len(*requests) != 16 {
		t.Fatalf("fourteen sources plus pagination and retry expected: %v", requests)
	}
	if result.Event.Status != "confirmed" || len(w.plan.Ends) != 14 || !w.plan.StartedAt.Equal(task.StartedAt) {
		t.Fatal("publication and progress did not complete")
	}
}

func TestT12_11AnySourceSecondCopyOrEvidenceFailureBlocksFirstPublication(t *testing.T) {
	for _, failure := range []string{"plan", "source", "target", "build-m", "ready-m", "build-s", "ready-s", "stale-evidence", "progress-plan"} {
		t.Run(failure, func(t *testing.T) {
			i, task, w, _ := initialization(t, failure)
			if _, err := i.Run(context.Background(), task); err == nil {
				t.Fatal("failed initialization accepted")
			}
			if w.intent.ID != "" {
				t.Fatal("incomplete copies published")
			}
			if failure == "source" && w.builds != 0 {
				t.Fatal("partial fourteen-source acquisition reached build")
			}
		})
	}
}

func TestT12_15ResumeOnlyConfirmsPublicationAndProgressWithoutRebuilding(t *testing.T) {
	for _, failure := range []string{"apply", "confirm", "progress"} {
		t.Run(failure, func(t *testing.T) {
			i, task, w, requests := initialization(t, failure)
			out, err := i.Run(context.Background(), task)
			if err == nil || out.Event.ID == "" {
				t.Fatal("lost durable event needed for recovery")
			}
			beforeBuilds, beforeRequests := w.builds, len(*requests)
			w.fail = ""
			w.trace = nil
			f := update.Finisher{Store: w, Delivery: w, Clock: i.Clock}
			if err = f.Finish(context.Background(), out.Event); err != nil {
				t.Fatal(err)
			}
			if w.builds != beforeBuilds || len(*requests) != beforeRequests || !reflect.DeepEqual(w.trace, []string{"apply", "confirm", "progress"}) {
				t.Fatal("resume touched acquisition or slots")
			}
		})
	}
}

func TestT12_14FullPaginationAndRetriesAlwaysOmitWindow(t *testing.T) {
	i, task, _, requests := initialization(t, "")
	if _, err := i.Run(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, request := range *requests {
		counts[request]++
	}
	if counts["dwd_xqjbsjzlb:1"] != 2 || counts["dwd_xqjbsjzlb:2"] != 1 {
		t.Fatal(fmt.Sprint(counts))
	}
}
