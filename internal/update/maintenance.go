package update

import (
	"context"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type MaintenanceStore interface {
	RecordMaintenance(context.Context, string, bool, time.Time) error
}
type MaintenanceReport struct {
	Cleaned, Recorded bool
	Failure           string
}
type Maintenance struct {
	Collector Collector
	Store     MaintenanceStore
	Clock     ports.Clock
}

// Run reports maintenance independently. The caller keeps the already confirmed
// task result even if cleanup or recording fails; no success rollback API exists.
func (m Maintenance) Run(ctx context.Context, plan control.CollectionPlan) MaintenanceReport {
	if m.Store == nil || m.Clock == nil {
		return MaintenanceReport{Failure: "maintenance_unconfigured"}
	}
	report := MaintenanceReport{Cleaned: m.Collector.Collect(ctx, plan) == nil}
	if !report.Cleaned {
		report.Failure = "cleanup_failed"
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	report.Recorded = m.Store.RecordMaintenance(cleanup, plan.Resource.ID, report.Cleaned, m.Clock.Now()) == nil
	if !report.Recorded {
		report.Failure = "maintenance_record_failed"
	}
	return report
}
