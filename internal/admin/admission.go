package admin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type AdmissionStore interface {
	Admit(context.Context, control.Request) (control.Acceptance, error)
}
type Admission struct {
	Store  AdmissionStore
	Clock  ports.Clock
	Random ports.Random
}
type Receipt struct {
	TaskID     string    `json:"task_id"`
	Status     string    `json:"status"`
	AcceptedAt time.Time `json:"accepted_at"`
	Created    bool      `json:"created"`
}

func requestKey(r *http.Request) (string, bool) {
	var values []string
	for k, v := range r.Header {
		if strings.EqualFold(k, "Idempotency-Key") {
			values = append(values, v...)
		}
	}
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 128 {
		return "", false
	}
	for _, c := range values[0] {
		if c < 33 || c > 126 || c == ',' {
			return "", false
		}
	}
	return values[0], true
}
func emptyBody(w http.ResponseWriter, r *http.Request) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	var body map[string]json.RawMessage
	err := d.Decode(&body)
	if err == io.EOF {
		return true
	}
	if err != nil || body == nil || len(body) != 0 {
		return false
	}
	return d.Decode(new(any)) == io.EOF
}
func (a Admission) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	operator, action, ok := Identity(r.Context())
	if !ok || (action != Incremental && action != ForceFull && action != Rerun) {
		respond(w, 403, "forbidden", "Action not permitted", nil)
		return
	}
	if a.Store == nil || a.Clock == nil || a.Random == nil {
		respond(w, 503, "unavailable", "Management service unavailable", nil)
		return
	}
	if r.Method != "POST" {
		respond(w, 405, "method_not_allowed", "POST required", nil)
		return
	}
	key, ok := requestKey(r)
	if !ok || !emptyBody(w, r) {
		respond(w, 400, "invalid_argument", "Idempotency-Key and an empty JSON object are required", nil)
		return
	}
	var random [16]byte
	n, err := a.Random.Read(random[:])
	if err != nil || n != len(random) {
		respond(w, 503, "unavailable", "Task identity unavailable", nil)
		return
	}
	mode := string(action)
	request := control.Request{ID: hex.EncodeToString(random[:]), IdempotencyKey: key, Mode: mode, RequestedBy: operator, AcceptedAt: a.Clock.Now()}
	var result control.Acceptance
	if table := r.PathValue("table"); table != "" && action == ForceFull {
		store, ok := a.Store.(interface {
			AdmitTable(context.Context, control.Request) (control.Acceptance, error)
		})
		if !ok {
			respond(w, 503, "unavailable", "Table update unavailable", nil)
			return
		}
		request.Table = table
		result, err = store.AdmitTable(r.Context(), request)
	} else if action == Rerun {
		store, ok := a.Store.(interface {
			RerunTask(context.Context, control.Request) (control.Acceptance, error)
		})
		if !ok {
			respond(w, 503, "unavailable", "Task rerun unavailable", nil)
			return
		}
		predecessor := r.PathValue("task_id")
		request.PredecessorID = &predecessor
		request.Mode = ""
		result, err = store.RerunTask(r.Context(), request)
	} else {
		result, err = a.Store.Admit(r.Context(), request)
	}
	if err != nil {
		writeAdmissionError(w, err)
		return
	}
	if result.Task.ID == "" {
		respond(w, 503, "unavailable", "Task acceptance unconfirmed", nil)
		return
	}
	w.Header().Set("Location", "/admin/v1/tasks/"+result.Task.ID)
	respond(w, 202, "accepted", "Task accepted", Receipt{result.Task.ID, result.Task.Status, result.Task.AcceptedAt, result.Created})
}
func writeAdmissionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, control.ErrMissing):
		respond(w, 404, "not_found", "Task not found", nil)
	case errors.Is(err, control.ErrNotFailed):
		respond(w, 409, "not_failed", "Only failed tasks can be rerun", nil)
	case errors.Is(err, control.ErrInitializeRequired):
		respond(w, 409, "initialize_required", "Automatic initialization is not complete", nil)
	case errors.Is(err, control.ErrAlreadyInitialized):
		respond(w, 409, "already_initialized", "Complete data is already initialized", nil)
	case errors.Is(err, control.ErrBusy):
		respond(w, 409, "busy", "An update or recovery is already pending", nil)
	case errors.Is(err, control.ErrIdempotency):
		respond(w, 409, "idempotency_conflict", "Idempotency key belongs to another request", nil)
	case errors.Is(err, control.ErrEntity):
		respond(w, 400, "invalid_argument", "Invalid task request", nil)
	default:
		respond(w, 503, "unavailable", "Task acceptance unconfirmed", nil)
	}
}
