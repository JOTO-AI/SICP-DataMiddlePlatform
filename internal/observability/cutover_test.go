package observability_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

type modeJournal struct {
	*journal
	mode, legacy string
	failMode     bool
}

func (j *modeJournal) Mode(context.Context) (string, error)              { return j.mode, nil }
func (j *modeJournal) LegacyTarget(context.Context) (string, error)      { return j.legacy, nil }
func (j *modeJournal) SetLegacyTarget(_ context.Context, v string) error { j.legacy = v; return nil }
func (j *modeJournal) SetMode(_ context.Context, v string) error {
	if j.failMode {
		return errors.New("mode write failed")
	}
	j.mode = v
	return nil
}
func cutoverCapacity() observability.CapacityConfig {
	return observability.CapacityConfig{MaxBytes: 1 << 20, MaxFiles: 100, WarningPercent: 80, Policy: observability.RejectNew}
}

func TestT28_13CutoverPreservesOriginalTargetUntilAcknowledged(t *testing.T) {
	j := &modeJournal{journal: newJournal()}
	s := sanitizer(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if observability.RegisterLegacy(ctx, j, s, logRegistry(t)) != nil {
		t.Fatal("legacy pin")
	}
	safe := sourceEvent(t, events.Call, "old-call")
	if j.Put(ctx, safe.ID(), safe.Bytes()) != nil {
		t.Fatal("seed")
	}
	db := &logsDB{lostReply: true}
	r := observability.Repository{Registry: logRegistry(t, true), Database: db}
	if observability.PrepareDual(ctx, j, s, cutoverCapacity(), r) != observability.ErrCutover || j.mode != "" || len(j.files) != 1 || len(db.rows) != 1 {
		t.Fatal("unconfirmed cutover marked complete")
	}
	if err := observability.PrepareDual(ctx, j, s, cutoverCapacity(), r); err != nil || j.mode == "" || len(j.files) != 0 || len(db.rows) != 1 {
		t.Fatal("acknowledged replay failed", err)
	}
	for _, dsn := range db.connections {
		if dsn != r.Registry.Logs().DSN() {
			t.Fatal("old call moved to upstream")
		}
	}
	if string(safe.Bytes()) != db.rows[safe.ID()] {
		t.Fatal("old payload rehashed")
	}
	if observability.RegisterLegacy(ctx, j, s, logRegistry(t)) != observability.ErrCutover {
		t.Fatal("rollback admitted legacy producer")
	}
	before := len(db.connections)
	if observability.PrepareDual(ctx, j, s, cutoverCapacity(), r) != nil || len(db.connections) != before {
		t.Fatal("restart attempted reclassification")
	}
}

func TestT28_13UnmarkedOrOrphanedJournalCannotCompleteSwitch(t *testing.T) {
	for _, scenario := range []string{"unmarked", "wrong-target", "orphan", "marker-failed"} {
		t.Run(scenario, func(t *testing.T) {
			j := &modeJournal{journal: newJournal()}
			s := sanitizer(t)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if scenario != "unmarked" {
				if observability.RegisterLegacy(ctx, j, s, logRegistry(t)) != nil {
					t.Fatal("pin")
				}
			}
			safe := sourceEvent(t, events.Call, "old")
			_ = j.Put(ctx, safe.ID(), safe.Bytes())
			switch scenario {
			case "wrong-target":
				j.legacy = s.Correlation("other-target")
			case "orphan":
				j.files["orphan"] = []byte("pending-unknown")
			case "marker-failed":
				j.failMode = true
			}
			db := &logsDB{}
			r := observability.Repository{Registry: logRegistry(t, true), Database: db}
			if observability.PrepareDual(ctx, j, s, cutoverCapacity(), r) != observability.ErrCutover || j.mode != "" {
				t.Fatal("incomplete switch accepted")
			}
			if scenario == "unmarked" || scenario == "wrong-target" {
				if len(db.connections) != 0 || len(j.files) != 1 {
					t.Fatal("unverified original target touched")
				}
			}
			if scenario == "marker-failed" {
				j.failMode = false
				if observability.PrepareDual(ctx, j, s, cutoverCapacity(), r) != nil || len(db.rows) != 1 {
					t.Fatal("marker retry duplicated delivery")
				}
			}
		})
	}
}

func TestT28_13RealJournalOwnershipAndModeSurviveReopen(t *testing.T) {
	path := t.TempDir()
	j, err := observability.OpenDirectory(path, testkit.FixedRandom(4), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := observability.OpenDirectory(path, testkit.FixedRandom(5), 4096); err == nil {
		_ = other.Close()
		t.Fatal("two writers own one journal")
	}
	s := sanitizer(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if observability.RegisterLegacy(ctx, j, s, logRegistry(t)) != nil {
		t.Fatal("pin")
	}
	r := observability.Repository{Registry: logRegistry(t, true), Database: &logsDB{}}
	if observability.PrepareDual(ctx, j, s, cutoverCapacity(), r) != nil {
		t.Fatal("empty cutover")
	}
	mode, err := j.Mode(ctx)
	if err != nil || mode == "" {
		t.Fatal("no durable mode")
	}
	if j.Close() != nil {
		t.Fatal("close")
	}
	j, err = observability.OpenDirectory(path, testkit.FixedRandom(6), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	got, err := j.Mode(ctx)
	if err != nil || got != mode {
		t.Fatal("mode lost on reopen")
	}
	usage, err := j.Usage(ctx)
	if err != nil || usage.Files != 0 || usage.Bytes != 0 {
		t.Fatal("metadata counted as unconfirmed events")
	}
}
