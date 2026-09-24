package worker_test

import (
	"context"
	"errors"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/assembly"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/worker"
)

type terminationDB struct {
	assembly.Database
	own, foreign int
	stopped      bool
	err          error
}

func (d *terminationDB) OwnSQLStopped(context.Context, string) (bool, error) {
	d.own++
	return d.stopped, d.err
}
func (d *terminationDB) ProcessSQLStopped(context.Context, string) (bool, error) {
	d.foreign++
	return d.stopped, d.err
}
func TestT15_10RecoveryRequiresVolumeExclusionAndActualSQLTermination(t *testing.T) {
	for _, scenario := range []string{"live-worker", "foreign-sql", "same-sql", "sql-error", "terminated"} {
		t.Run(scenario, func(t *testing.T) {
			db := &terminationDB{stopped: scenario == "terminated"}
			r := &assembly.Runtime{Database: db, ProcessID: "new", Files: &assembly.FileSet{WorkerExclusive: scenario != "live-worker"}}
			owner := control.Ownership{TaskID: "task", OwnerID: "old", Generation: 1}
			if scenario == "same-sql" {
				owner.OwnerID = "new"
			}
			if scenario == "sql-error" {
				db.err = errors.New("unknown")
			}
			stopped, err := (worker.Termination{Runtime: r}).Stopped(t.Context(), owner)
			if stopped != (scenario == "terminated") || (err != nil) != (scenario == "sql-error") {
				t.Fatal(stopped, err)
			}
			if scenario == "live-worker" && db.own+db.foreign != 0 {
				t.Fatal("proof bypassed process exclusion")
			}
			if scenario == "same-sql" && (db.own != 1 || db.foreign != 0) {
				t.Fatal("same process proof incorrect")
			}
		})
	}
}
