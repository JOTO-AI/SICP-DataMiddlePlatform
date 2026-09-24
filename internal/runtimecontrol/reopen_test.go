package runtimecontrol_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type reopenDB struct {
	*database
	copy    update.Copy
	missing bool
}

func (d *reopenDB) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	return d, nil
}
func jsonText(v any) string { b, _ := json.Marshal(v); return string(b) }
func (d *reopenDB) QueryRow(ctx context.Context, q string, args ...any) storage.Row {
	switch {
	case strings.Contains(q, "SELECT (SELECT c.id"):
		if d.missing {
			return row{nil}
		}
		return row{"old-copy"}
	case strings.Contains(q, "FROM control.candidates"):
		return row{"old-copy", "old-task", "S", int16(0), int64(1), "C100", int64(1), "ready", d.at, jsonText(d.copy.Evidence)}
	case strings.Contains(q, "FROM control.bundles"):
		return row{"C100", jsonText(d.copy.Manifest), d.at, d.at, d.at}
	case strings.Contains(q, "FROM control.slots"):
		return row{"S", int16(0), int64(1), "C100", false, "ready"}
	}
	return d.database.QueryRow(ctx, q, args...)
}
func TestT15_10ClosedPublishedStandbyRequiresSuccessfulFixedQueryChecks(t *testing.T) {
	for _, scenario := range []string{"success", "checks-failed", "unpublished", "pending"} {
		t.Run(scenario, func(t *testing.T) {
			s, base, _ := gateFixture(t)
			m := candidate.Manifest{Combination: "C100", Input: staging.SealRef{RunID: "old-run", SHA256: strings.Repeat("a", 64)}}
			for _, table := range source.Catalog() {
				m.Tables = append(m.Tables, candidate.TableVersion{Table: table.Name, Scope: "synthetic", SHA256: strings.Repeat("a", 64)})
			}
			target := source.Target{Position: storage.Position{Instance: storage.Standby, Slot: 0}, Generation: 1, CandidateID: "old-copy"}
			ev := candidate.Evidence{Target: target, Combination: "C100", CodeVersion: "v1", CheckedAt: base.at, Queries: map[contract.Operation]candidate.QueryEvidence{}}
			for _, def := range contract.Definitions() {
				names, _ := metadata.Dependencies(def.Operation)
				deps := map[string]string{}
				for _, name := range names {
					deps[name] = "synthetic:" + strings.Repeat("a", 64)
				}
				ev.Queries[def.Operation] = candidate.QueryEvidence{Dependencies: deps, Cases: 1, SHA256: strings.Repeat("b", 64)}
			}
			d := &reopenDB{database: base, copy: update.Copy{Target: target, Manifest: m, Evidence: ev}, missing: scenario == "unpublished"}
			s.Repository.Database = d
			base.pending = scenario == "pending"
			origin := control.ActiveMetadata{Sequence: 1, EventID: "old", TaskID: "old-task", OwnerGeneration: 1, Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 1, BundleID: "C100"}, CodeVersion: "v1"}
			p := control.UpdatePlan{TaskID: "task", Mode: "incremental", StartedAt: base.at, Origin: origin, First: storage.Standby, Second: storage.Main, Overlap: time.Minute, Windows: map[string]control.SourceWindow{}}
			for _, table := range source.Catalog() {
				p.Windows[table.Name] = control.SourceWindow{ConfirmedUntil: base.at, Start: base.at.Add(-time.Minute), End: base.at}
			}
			calls := 0
			err := s.ReopenPublished(t.Context(), p, func(ctx context.Context, old update.Copy, gate read.Gate, admission update.FixedAdmission) error {
				calls++
				if base.commits != 0 {
					t.Fatal("control exclusion ended before queries")
				}
				h := read.Handle{Position: old.Target.Position, Generation: old.Target.Generation, Combination: old.Manifest.Combination}
				if _, e := gate.BeginRead(ctx, h); e != nil {
					t.Fatal(e)
				}
				h.Generation++
				if _, e := admission.AcquireFixed(ctx, h); e == nil {
					t.Fatal("changed physical identity allowed")
				}
				if scenario == "checks-failed" {
					return errors.New("query role denied")
				}
				return nil
			})
			if (err == nil) != (scenario == "success") || (base.commits == 1) != (scenario == "success") {
				t.Fatal(err, base.commits)
			}
			writes := 0
			for _, q := range base.calls {
				if strings.HasPrefix(q, "UPDATE control.slots") {
					writes++
				}
			}
			if (writes == 1) != (scenario == "success") || (calls == 1) != (scenario == "success" || scenario == "checks-failed") {
				t.Fatal(writes, calls)
			}
		})
	}
}
