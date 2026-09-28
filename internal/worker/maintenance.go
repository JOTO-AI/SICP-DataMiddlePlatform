package worker

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

var maintenanceName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type retiredVersionFiles struct {
	staging, targets *staging.QuotaFiles
	manifest         candidate.Manifest
}

func (f retiredVersionFiles) DeleteRetired(ctx context.Context, r control.Resource) error {
	m := f.manifest
	if !r.Retired || r.Kind != "version" || r.ID != "version-"+m.Combination || r.BundleID != m.Combination || m.Combination != "C"+m.Input.RunID || m.Validate() != nil || !maintenanceName.MatchString(m.Combination) || f.staging == nil || f.targets == nil {
		return control.ErrEntity
	}
	store, err := staging.NewStore(m.Input.RunID, f.staging, 64<<20)
	if err != nil {
		return err
	}
	// The creating task is terminal and retirement excludes pending recovery.
	// Release only that task's lease; any other durable file lease still blocks
	// Cleanup. Interrupted cleanup resumes from its durable retirement marker.
	lease, err := store.Retain(ctx, m.Input, m.Input.RunID)
	if err == nil {
		err = lease.Release(ctx)
	}
	if err != nil && !errors.Is(err, staging.ErrReleased) && !errors.Is(err, staging.ErrRetired) {
		return err
	}
	if err := store.Cleanup(ctx, m.Input); err != nil {
		return err
	}
	prefix := "target-" + m.Combination + "."
	names, err := f.targets.List(ctx, prefix)
	if err != nil {
		return err
	}
	for _, name := range names {
		if !strings.HasPrefix(name, prefix) || strings.ContainsAny(name, `/\:`) {
			return control.ErrEntity
		}
		if err := f.targets.RemoveRetired(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// maintain runs only after a successful task is durable, before releasing its
// execution ownership. Failure is recorded separately and never undoes success.
func (w Executor) maintain(ctx context.Context, task control.Task, owner control.Ownership) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	repo := w.Runtime.Control.WithOwnership(owner)
	if task.Status == "success_published" {
		if err := repo.RegisterResource(ctx, control.Resource{ID: "version-C" + task.ID, Kind: "version", BundleID: "C" + task.ID}); err != nil {
			return err
		}
	}
	bundles, err := repo.MaintenanceBundles(ctx, 32)
	if err != nil {
		return err
	}
	for _, b := range bundles {
		var manifest candidate.Manifest
		if json.Unmarshal([]byte(b.Tables), &manifest) != nil || manifest.Combination != b.ID || manifest.Validate() != nil {
			return control.ErrEntity
		}
		plan, err := repo.PlanCollection(ctx, "version-"+b.ID)
		if errors.Is(err, control.ErrBusy) {
			continue
		}
		if err != nil {
			return err
		}
		files := retiredVersionFiles{staging: w.Runtime.Files.Staging, targets: w.Runtime.Files.Targets, manifest: manifest}
		report := (update.Maintenance{Collector: update.Collector{Store: repo, Files: files}, Store: repo, Clock: w.Clock}).Run(ctx, plan)
		if report.Failure != "" {
			return errors.New(report.Failure)
		}
	}
	return nil
}
