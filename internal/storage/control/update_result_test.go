package control_test

import (
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func TestT13_17SuccessfulUpdatePreservesSourceEndPublicationTimeAndSeparateCounts(t *testing.T) {
	r, d, p, i := progressFixture(t)
	p.StartedAt = p.StartedAt.Add(2 * time.Hour)
	p.CheckedAt = p.StartedAt.Add(12 * time.Minute)
	p.Mode = "incremental"
	d.task.Mode = p.Mode
	d.task.StartedAt = &p.StartedAt
	for name := range p.Ends {
		p.Ends[name] = p.StartedAt
	}
	summary := control.ResultSummary{Sources: map[string]control.SourceCounts{}, Copies: [2]control.CopyCounts{{WrittenRows: 50, RebuiltTables: 14}, {WrittenRows: 50, RebuiltTables: 14}}}
	for _, table := range source.Catalog() {
		summary.Sources[table.Name] = control.SourceCounts{}
	}
	summary.Sources[source.Catalog()[0].Name] = control.SourceCounts{Before: 49, After: 50, Added: 1, Modified: 2}
	var err error
	p.Summary, err = control.SummaryDocument(summary)
	if err != nil {
		t.Fatal(err)
	}
	i.CreatedAt = p.CheckedAt
	if err = r.StageProgress(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	e, _, err := r.EnqueuePublication(t.Context(), i)
	if err != nil {
		t.Fatal(err)
	}
	published := p.StartedAt.Add(13 * time.Minute)
	if err = r.RecordPublicationApplied(t.Context(), e, published); err != nil {
		t.Fatal(err)
	}
	e, err = r.ConfirmPublication(t.Context(), e, published)
	if err != nil {
		t.Fatal(err)
	}
	if d.task.FinishedAt != nil || len(d.progress) != 0 {
		t.Fatal("published treated as complete")
	}
	if err = r.CompletePublished(t.Context(), e, published.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if d.task.PublishedAt.Format("15:04") != "10:13" || d.task.CheckedAt.Format("15:04") != "10:12" {
		t.Fatal(d.task)
	}
	for _, v := range d.progress {
		if v.ConfirmedUntil.Format("15:04") != "10:00" {
			t.Fatal(v)
		}
	}
	var got control.ResultSummary
	if json.Unmarshal([]byte(d.task.Summary), &got) != nil || got.Copies != summary.Copies || !maps.Equal(got.Sources, summary.Sources) {
		t.Fatal(d.task.Summary)
	}
}
func TestT13_20BackwardSingleSourceRollsBackWholePublishedAndUnchangedConfirmation(t *testing.T) {
	for _, kind := range []string{"published", "unchanged"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "unchanged" {
				r, d, a, copies := unchangedFixture(t)
				for _, table := range source.Catalog() {
					d.progress[table.Name] = control.Progress{Source: table.Name, ConfirmedUntil: d.task.StartedAt.Add(-time.Hour), CheckedAt: d.task.StartedAt.Add(-time.Hour)}
				}
				name := source.Catalog()[13].Name
				v := d.progress[name]
				v.ConfirmedUntil = d.task.StartedAt.Add(time.Minute)
				d.progress[name] = v
				before := maps.Clone(d.progress)
				if err := r.CompleteUnchanged(t.Context(), a, copies, d.task.StartedAt.Add(time.Hour)); err == nil || !maps.Equal(before, d.progress) || len(d.checks) != 0 || d.task.Status != "running" {
					t.Fatal(err, d.task)
				}
				return
			}
			r, d, p, i := progressFixture(t)
			e := appliedEvent(t, r, p, i)
			at := p.StartedAt.Add(time.Hour)
			e, err := r.ConfirmPublication(t.Context(), e, at)
			if err != nil {
				t.Fatal(err)
			}
			for _, table := range source.Catalog() {
				d.progress[table.Name] = control.Progress{Source: table.Name, ConfirmedUntil: p.StartedAt.Add(-time.Hour), CheckedAt: p.StartedAt.Add(-time.Hour)}
			}
			name := source.Catalog()[13].Name
			v := d.progress[name]
			v.ConfirmedUntil = p.StartedAt.Add(time.Minute)
			d.progress[name] = v
			before := maps.Clone(d.progress)
			if err = r.CompletePublished(t.Context(), e, at); err == nil || !maps.Equal(before, d.progress) || d.task.Status != "published_pending_progress" || d.task.FinishedAt != nil {
				t.Fatal(err, d.task)
			}
		})
	}
}
