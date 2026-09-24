package update_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type planner struct {
	plan control.UpdatePlan
	err  error
}

func (p planner) PlanUpdate(context.Context, control.ActiveMetadata, time.Duration) (control.UpdatePlan, error) {
	return p.plan, p.err
}

type preparationSpy struct {
	calls int
	next  update.TargetPreparer
}

func (s *preparationSpy) Changes(ctx context.Context, m candidate.Manifest) (map[string]candidate.ChangeCounts, error) {
	return s.next.(update.ChangeCounter).Changes(ctx, m)
}

func (s *preparationSpy) Prepare(ctx context.Context, o candidate.PrepareOptions, l *staging.Lease) (candidate.Manifest, error) {
	s.calls++
	return s.next.Prepare(ctx, o, l)
}

func updatePlan() control.UpdatePlan {
	at := time.Date(2100, 1, 1, 10, 0, 0, 0, time.UTC)
	p := control.UpdatePlan{TaskID: "update", Mode: "incremental", StartedAt: at, First: storage.Standby, Second: storage.Main, Overlap: 5 * time.Minute, Windows: map[string]control.SourceWindow{}, Origin: control.ActiveMetadata{Sequence: 1, EventID: "original", TaskID: "old", OwnerGeneration: 1, CodeVersion: "v1", Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 1, BundleID: "C100"}}}
	for _, table := range ingest.Catalog() {
		p.Windows[table.Name] = control.SourceWindow{ConfirmedUntil: at.Add(-2 * time.Hour), Start: at.Add(-125 * time.Minute), End: at}
	}
	return p
}

type requestRecord struct {
	name  string
	query url.Values
}

func acquisition(t *testing.T, failed string) (update.Acquirer, *[]requestRecord) {
	t.Helper()
	i, _, _, _ := initialization(t, "")
	a := i.Source.(update.Acquirer)
	clock := &timer{now: updatePlan().StartedAt}
	requests := []requestRecord{}
	tries := map[string]int{}
	client, err := ingest.NewClient("https://mock.invalid", "synthetic", roundTrip(func(r *http.Request) (*http.Response, error) {
		name := path.Base(r.URL.Path)
		q := r.URL.Query()
		requests = append(requests, requestRecord{name, q})
		key := name + q.Get("page")
		tries[key]++
		status := 200
		body := `{"code":200,"message":"ok","data":{"total":0,"list":[]}}`
		if name == failed || name == "dwd_xqjbsjzlb" {
			pk := ""
			for _, d := range ingest.Catalog() {
				if d.Name == name {
					pk = d.PrimaryKey
				}
			}
			if q.Get("page") == "1" {
				body = fmt.Sprintf(`{"code":200,"message":"ok","data":{"total":3,"list":[{"%s":"a"},{"%s":"b"}]}}`, pk, pk)
			} else {
				body = fmt.Sprintf(`{"code":200,"message":"ok","data":{"total":3,"list":[{"%s":"c"}]}}`, pk)
			}
			if q.Get("page") == "2" && (name == failed || tries[key] == 1) {
				status = 503
			}
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
	a.Collector = collector
	a.Budget, err = ingest.NewTaskBudget(clock.Now().Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	return a, &requests
}
func TestT13_12EachSourceFinalFailureBlocksComparisonAndAllLaterWork(t *testing.T) {
	for _, table := range ingest.Catalog() {
		t.Run(table.Name, func(t *testing.T) {
			a, requests := acquisition(t, table.Name)
			p := updatePlan()
			before := p.Origin
			spy := &preparationSpy{}
			f := update.Fetcher{Planner: planner{plan: p}, Source: a, Preparer: spy, Zone: time.UTC}
			out, err := f.Prepare(t.Context(), p.Origin)
			if err == nil || out.Lease != nil || out.Manifest.Combination != "" || spy.calls != 0 || p.Origin != before {
				t.Fatal(out, err, spy.calls)
			}
			last := (*requests)[len(*requests)-1]
			if last.name != table.Name || last.query.Get("page") != "2" {
				t.Fatal("did not fail after a confirmed partial page", *requests)
			}
			attempts := 0
			for _, r := range *requests {
				if r.name == table.Name && r.query.Get("page") == "2" {
					attempts++
				}
			}
			if attempts != 3 {
				t.Fatal("failure was not final", attempts)
			}
			if _, err = a.Staging.Seal(t.Context(), a.Limits); err == nil {
				t.Fatal("partial pages became a complete seal")
			}
		})
	}
}
func TestT13_01AcquisitionPagesAndRetriesKeepPerTableWindow(t *testing.T) {
	a, requests := acquisition(t, "")
	p := updatePlan()
	name := ingest.Catalog()[0].Name
	w := p.Windows[name]
	w.ConfirmedUntil = w.ConfirmedUntil.Add(-time.Hour)
	w.Start = w.Start.Add(-time.Hour)
	p.Windows[name] = w
	l, err := a.Acquire(t.Context(), p, time.UTC)
	if err != nil || l == nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, r := range *requests {
		start := "2100-01-01 07:55:00"
		if r.name == name {
			start = "2100-01-01 06:55:00"
		}
		if r.query.Get("startTime") != start || r.query.Get("endTime") != "2100-01-01 10:00:00" {
			t.Fatal(r)
		}
		counts[r.name+":"+r.query.Get("page")]++
	}
	if counts["dwd_xqjbsjzlb:2"] != 2 || len(l.Pages()) != 15 {
		t.Fatal(counts, len(l.Pages()))
	}
}
func TestT13_18And19InvalidPlanNeverCallsSourceOrPreparation(t *testing.T) {
	for _, mode := range []string{"force_full", "incremental"} {
		t.Run(mode, func(t *testing.T) {
			a, requests := acquisition(t, "")
			p := updatePlan()
			p.Mode = mode
			name := ingest.Catalog()[0].Name
			w := p.Windows[name]
			w.ConfirmedUntil = p.StartedAt.Add(time.Minute)
			w.Start = w.ConfirmedUntil.Add(-p.Overlap)
			p.Windows[name] = w
			spy := &preparationSpy{}
			_, err := (update.Fetcher{Planner: planner{plan: p}, Source: a, Preparer: spy, Zone: time.UTC}).Prepare(t.Context(), p.Origin)
			if !errors.Is(err, control.ErrTime) || len(*requests) != 0 || spy.calls != 0 {
				t.Fatal(err)
			}
		})
	}
}
func TestT12_14ForcedAcquisitionOmitsWindowOnEveryPageAndRetry(t *testing.T) {
	a, requests := acquisition(t, "")
	p := updatePlan()
	p.Mode = "force_full"
	l, err := a.Acquire(t.Context(), p, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range *requests {
		if r.query.Has("startTime") || r.query.Has("endTime") {
			t.Fatal(r)
		}
	}
	for _, page := range l.Pages() {
		if page.Request.Mode != ingest.ForceFull {
			t.Fatal(page)
		}
	}
}
func TestT13_12FetcherUsesCompleteSealBeforeActualPreparation(t *testing.T) {
	a, _ := acquisition(t, "")
	i, initialTask, _, _ := initialization(t, "")
	p := updatePlan()
	p.Mode = "force_full"
	spy := &preparationSpy{next: i.Preparer}
	o := i.Options
	o.Combination = "C101"
	baseline, err := i.Run(t.Context(), initialTask)
	if err != nil {
		t.Fatal(err)
	}
	p.Origin.Position.BundleID = baseline.Manifest.Combination
	o.Base = &baseline.Manifest
	o.BaseSource = i.Preparer.(candidate.TargetFiles)
	f := update.Fetcher{Planner: planner{plan: p}, Source: a, Preparer: spy, Options: o, Zone: time.UTC}
	out, err := f.Prepare(t.Context(), p.Origin)
	if err != nil || spy.calls != 1 || len(out.Manifest.Tables) != 14 || !reflect.DeepEqual(out.Plan, p) {
		t.Fatal(out, err)
	}
}

func TestT13_16FullUpdateWithoutTrustedOldBaselineCannotPrepareTarget(t *testing.T) {
	a, _ := acquisition(t, "")
	p := updatePlan()
	p.Mode = "force_full"
	spy := &preparationSpy{}
	out, err := (update.Fetcher{Planner: planner{plan: p}, Source: a, Preparer: spy, Zone: time.UTC}).Prepare(t.Context(), p.Origin)
	if err == nil || spy.calls != 0 || out.Manifest.Combination != "" {
		t.Fatal(out, err)
	}
}
