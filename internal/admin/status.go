package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type StatusStore interface {
	TaskStatus(context.Context, string) (control.TaskStatus, error)
}
type StatusHandler struct {
	Store     StatusStore
	Selection interface {
		TableSelection(context.Context, string) (string, error)
	}
	Diagnostics interface {
		FailedAttempts(context.Context, string) ([]observability.FailedAttempt, error)
	}
	Acquisition interface {
		AcquisitionProgress(context.Context, string) (*control.AcquisitionProgress, error)
	}
	Work interface {
		WorkProgress(context.Context, string) (*activity.Snapshot, error)
	}
}
type TaskView struct {
	ID                     string                           `json:"task_id"`
	Mode                   string                           `json:"mode"`
	Status                 string                           `json:"status"`
	Operator               string                           `json:"operator"`
	Predecessor            *string                          `json:"predecessor_id"`
	AcceptedAt             time.Time                        `json:"accepted_at"`
	StartedAt              *time.Time                       `json:"started_at"`
	ReadyAt                *time.Time                       `json:"ready_at"`
	PublishedAt            *time.Time                       `json:"published_at"`
	CheckedAt              *time.Time                       `json:"checked_at"`
	FinishedAt             *time.Time                       `json:"finished_at"`
	ProgressConfirmed      bool                             `json:"progress_confirmed"`
	Progress               map[string]time.Time             `json:"confirmed_until"`
	Counts                 *control.ResultSummary           `json:"counts"`
	Failure                string                           `json:"failure,omitempty"`
	SelectedTable          string                           `json:"selected_table,omitempty"`
	FailedAttempts         []observability.FailedAttempt    `json:"failed_attempts,omitempty"`
	DiagnosticsUnavailable bool                             `json:"diagnostics_unavailable,omitempty"`
	DiagnosticSources      []observability.DiagnosticSource `json:"diagnostic_sources,omitempty"`
	Acquisition            *control.AcquisitionProgress     `json:"acquisition,omitempty"`
	WorkProgress           *activity.Snapshot               `json:"work_progress,omitempty"`
}

func (s StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, action, ok := Identity(r.Context())
	if !ok || action != Status {
		respond(w, 403, "forbidden", "Action not permitted", nil)
		return
	}
	if s.Store == nil {
		respond(w, 503, "unavailable", "Task status unavailable", nil)
		return
	}
	out, err := s.Store.TaskStatus(r.Context(), r.PathValue("task_id"))
	if err != nil {
		if errors.Is(err, control.ErrMissing) {
			respond(w, 404, "not_found", "Task not found", nil)
		} else {
			respond(w, 503, "unavailable", "Task status unavailable", nil)
		}
		return
	}
	task := out.Task
	view := TaskView{ID: task.ID, Mode: task.Mode, Status: task.Status, Operator: task.RequestedBy, Predecessor: task.PredecessorID, AcceptedAt: task.AcceptedAt, StartedAt: task.StartedAt, ReadyAt: task.ReadyAt, PublishedAt: task.PublishedAt, CheckedAt: task.CheckedAt, FinishedAt: task.FinishedAt, ProgressConfirmed: out.Progress != nil}
	if s.Selection != nil {
		view.SelectedTable, err = s.Selection.TableSelection(r.Context(), task.ID)
		if err != nil {
			respond(w, 503, "unavailable", "Task scope unavailable", nil)
			return
		}
	}
	if out.Progress != nil {
		view.Progress = out.Progress.Ends
	}
	if s.Acquisition != nil {
		view.Acquisition, err = s.Acquisition.AcquisitionProgress(r.Context(), task.ID)
		if err != nil {
			respond(w, 503, "unavailable", "Task progress unavailable", nil)
			return
		}
	}
	if s.Work != nil {
		view.WorkProgress, err = s.Work.WorkProgress(r.Context(), task.ID)
		if err != nil {
			respond(w, 503, "unavailable", "Task execution status unavailable", nil)
			return
		}
	}
	// Decode into a whitelist: arbitrary task summary, roles, source payloads,
	// idempotency keys and executor credentials never enter the status response.
	if len(task.Summary) <= 16<<10 {
		var counts control.ResultSummary
		if json.Unmarshal([]byte(task.Summary), &counts) == nil && counts.Validate() == nil {
			view.Counts = &counts
		}
		var failure struct {
			Failure string `json:"failure"`
		}
		if json.Unmarshal([]byte(task.Summary), &failure) == nil {
			switch failure.Failure {
			case "acquisition_failed", "corrupt_seal", "interrupted":
				view.Failure = failure.Failure
			}
		}
	}
	if s.Diagnostics != nil {
		if detailed, ok := s.Diagnostics.(interface {
			FailedAttemptReport(context.Context, string) (observability.DiagnosticReport, error)
		}); ok {
			report, err := detailed.FailedAttemptReport(r.Context(), task.ID)
			view.DiagnosticsUnavailable = err != nil || !report.Complete
			if err == nil {
				view.FailedAttempts = report.Items
				view.DiagnosticSources = report.Sources
			}
		} else {
			attempts, err := s.Diagnostics.FailedAttempts(r.Context(), task.ID)
			if err != nil {
				view.DiagnosticsUnavailable = true
			} else {
				view.FailedAttempts = attempts
			}
		}
	}
	respond(w, 200, "ok", "OK", view)
}
