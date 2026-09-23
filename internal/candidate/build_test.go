package candidate_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type ledger struct {
	baseline         candidate.Baseline
	trace            []string
	complete, failed bool
	failInvalidate   bool
}

func (l *ledger) Inspect(context.Context, source.Target) (candidate.Baseline, error) {
	l.trace = append(l.trace, "inspect")
	return l.baseline, nil
}
func (l *ledger) Invalidate(_ context.Context, _ source.Target, table string) error {
	l.trace = append(l.trace, "invalidate:"+table)
	if l.failInvalidate {
		return errors.New("commit unknown")
	}
	delete(l.baseline.Tables, table)
	return nil
}
func (l *ledger) Record(_ context.Context, _ source.Target, e candidate.TableEvidence) error {
	l.trace = append(l.trace, "record:"+e.Version.Table)
	l.baseline.Tables[e.Version.Table] = e
	return nil
}
func (l *ledger) Complete(context.Context, source.Target, candidate.Manifest) error {
	l.trace = append(l.trace, "complete")
	l.complete = true
	return nil
}
func (l *ledger) Fail(context.Context, source.Target) error { l.failed = true; return nil }

type rows struct {
	values []ingest.Record
	next   int
	closed *int
}

func (r *rows) Next(context.Context) (ingest.Record, error) {
	if r.next == len(r.values) {
		return ingest.Record{}, io.EOF
	}
	v := r.values[r.next]
	r.next++
	return v, nil
}
func (r *rows) Close(context.Context) error { *r.closed++; return nil }

type completeSource struct {
	manifest                 candidate.Manifest
	tables                   map[string][]ingest.Record
	opens, closed, validated int
	invalid                  bool
}

func (s *completeSource) Validate(_ context.Context, m candidate.Manifest) error {
	s.validated++
	if s.invalid || m.Combination != s.manifest.Combination || m.Input != s.manifest.Input {
		return candidate.ErrBuild
	}
	return nil
}
func (s *completeSource) Open(_ context.Context, _ candidate.Manifest, v candidate.TableVersion) (candidate.Rows, error) {
	s.opens++
	return &rows{values: s.tables[v.Table], closed: &s.closed}, nil
}

type writer struct {
	l      *ledger
	writes map[string][]source.SourceRow
	calls  int
	fail   bool
}

func (w *writer) Rebuild(ctx context.Context, _ source.Target, table string, it source.Iterator, _ int) (source.Report, error) {
	w.calls++
	w.l.trace = append(w.l.trace, "write:"+table)
	if previous, ok := w.l.baseline.Tables[table]; ok && previous.Complete {
		return source.Report{}, errors.New("old evidence still valid before physical write")
	}
	for {
		row, more, err := it.Next(ctx)
		if err != nil {
			return source.Report{}, err
		}
		if !more {
			break
		}
		w.writes[table] = append(w.writes[table], row)
		if w.fail {
			return source.Report{}, errors.New("half written")
		}
	}
	return source.Report{WrittenRows: len(w.writes[table]), CompletedBatches: 1, Complete: true}, nil
}
func buildFixture(t *testing.T) (candidate.Builder, source.Target, candidate.Manifest, *ledger, *completeSource, *writer) {
	t.Helper()
	target := source.Target{Position: storage.Position{Instance: storage.Standby, Slot: 1}, Generation: 8, CandidateID: "candidate-1"}
	m := candidate.Manifest{Combination: "C101", Input: staging.SealRef{RunID: "synthetic-run", SHA256: strings.Repeat("a", 64)}}
	s := &completeSource{tables: map[string][]ingest.Record{}}
	for i, table := range source.Catalog() {
		var values []ingest.Record
		if i == 0 {
			for _, key := range []string{"001", "002"} {
				encoded, _ := json.Marshal(map[string]string{table.PrimaryKey: key})
				r, err := ingest.DecodeRecord(table.Name, encoded)
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, r)
			}
		}
		hash, err := merge.SourceDigest(t.Context(), table.Name, "synthetic-school", values)
		if err != nil {
			t.Fatal(err)
		}
		m.Tables = append(m.Tables, candidate.TableVersion{Table: table.Name, Scope: "synthetic-school", SHA256: hash, Rows: int64(len(values))})
		s.tables[table.Name] = values
	}
	s.manifest = m
	l := &ledger{baseline: candidate.Baseline{State: source.BuildState{ActiveInstance: storage.Main, Generation: target.Generation, CandidateID: target.CandidateID, OwnerValid: true}, Tables: map[string]candidate.TableEvidence{}}}
	w := &writer{l: l, writes: map[string][]source.SourceRow{}}
	return candidate.Builder{Ledger: l, Source: s, Writer: w, BatchSize: 2}, target, m, l, s, w
}
func matching(l *ledger, m candidate.Manifest) {
	for _, v := range m.Tables {
		l.baseline.Tables[v.Table] = candidate.TableEvidence{Version: v, Complete: true}
	}
}

func TestT12_01MatchingCompleteSlotEvidenceReusesEveryTable(t *testing.T) {
	b, target, m, l, s, w := buildFixture(t)
	matching(l, m)
	got, err := b.Build(t.Context(), target, m, false)
	if err != nil || !got.StorageComplete || len(got.Reused) != 14 || len(got.Rebuilt) != 0 || w.calls != 0 || s.opens != 0 || s.validated != 2 || !l.complete {
		t.Fatal(got, err, l.trace)
	}
}
func TestT12_02LaggingMissingAndPartialTablesRebuildFromFullTarget(t *testing.T) {
	for _, kind := range []string{"lagging", "missing", "partial", "force"} {
		t.Run(kind, func(t *testing.T) {
			b, target, m, l, s, w := buildFixture(t)
			matching(l, m)
			table := m.Tables[0].Table
			switch kind {
			case "lagging":
				old := l.baseline.Tables[table]
				old.Version.SHA256 = strings.Repeat("b", 64)
				l.baseline.Tables[table] = old
			case "missing":
				delete(l.baseline.Tables, table)
			case "partial":
				old := l.baseline.Tables[table]
				old.Complete = false
				l.baseline.Tables[table] = old
			}
			got, err := b.Build(t.Context(), target, m, kind == "force")
			want := 1
			if kind == "force" {
				want = 14
			}
			if err != nil || !got.StorageComplete || len(got.Rebuilt) != want || len(w.writes[table]) != 2 || s.closed != want {
				t.Fatal(got, err, l.trace)
			}
			// Two source rows survive even if only one changed this round.
			pk := source.Catalog()[0].PrimaryKey
			if string(w.writes[table][0][pk]) != `"001"` || string(w.writes[table][1][pk]) != `"002"` {
				t.Fatal(w.writes[table])
			}
		})
	}
}
func TestT12_03UnsafePhysicalTargetNeverReusesOrWrites(t *testing.T) {
	for _, kind := range []string{"serving", "readers", "admission", "ownership", "generation", "candidate", "unknown-active", "false-initialization"} {
		t.Run(kind, func(t *testing.T) {
			b, target, m, l, s, w := buildFixture(t)
			matching(l, m)
			switch kind {
			case "serving":
				l.baseline.State.ActiveInstance = storage.Standby
			case "readers":
				l.baseline.State.Readers = 1
			case "admission":
				l.baseline.State.AdmissionOpen = true
			case "ownership":
				l.baseline.State.OwnerValid = false
			case "generation":
				l.baseline.State.Generation--
			case "candidate":
				l.baseline.State.CandidateID = "other"
			case "unknown-active":
				l.baseline.State.ActiveInstance = ""
			case "false-initialization":
				l.baseline.State.Initializing = true
			}
			got, err := b.Build(t.Context(), target, m, false)
			if !errors.Is(err, source.ErrTarget) || got.StorageComplete || l.complete || w.calls != 0 || s.opens != 0 || len(l.trace) != 1 {
				t.Fatal(got, err, l.trace)
			}
		})
	}
}
func TestT12_02UncommittedInvalidationAndHalfWriteNeverComplete(t *testing.T) {
	for _, kind := range []string{"invalidation", "write", "digest", "duplicate", "provenance"} {
		t.Run(kind, func(t *testing.T) {
			b, target, m, l, s, w := buildFixture(t)
			switch kind {
			case "invalidation":
				l.failInvalidate = true
			case "write":
				w.fail = true
			case "digest":
				m.Tables[0].SHA256 = strings.Repeat("b", 64)
			case "duplicate":
				table := m.Tables[0].Table
				s.tables[table][1] = s.tables[table][0]
			case "provenance":
				s.invalid = true
			}
			got, err := b.Build(t.Context(), target, m, false)
			if err == nil || got.StorageComplete || l.complete || len(l.baseline.Tables) != 0 {
				t.Fatal(got, err, l.trace)
			}
			if (kind == "invalidation" || kind == "provenance") && w.calls != 0 {
				t.Fatal("unconfirmed source or state wrote", l.trace)
			}
			if s.opens != s.closed {
				t.Fatal("leaked input", s.opens, s.closed)
			}
		})
	}
}
func TestT12_01ManifestRequiresFourteenUniqueSourceVersions(t *testing.T) {
	_, _, m, _, _, _ := buildFixture(t)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.Tables[1] = m.Tables[0]
	if m.Validate() == nil {
		t.Fatal("duplicate manifest table accepted")
	}
}
