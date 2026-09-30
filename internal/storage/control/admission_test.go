package control_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/admin"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

func management(t *testing.T, store admin.AdmissionStore, at time.Time, action admin.Action) http.Handler {
	t.Helper()
	auth, err := admin.NewAuth("query-synthetic", []admin.Credential{{Operator: "operator-1", Token: "management-synthetic-secret", Actions: []admin.Action{action}}})
	if err != nil {
		t.Fatal(err)
	}
	h, err := auth.Protect(action, admin.Admission{Store: store, Clock: testkit.FakeClock{Time: at}, Random: testkit.FixedRandom(3)})
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func managementRequest(ctx context.Context) *http.Request {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer management-synthetic-secret")
	r.Header.Set("Idempotency-Key", "request-key")
	return r
}
func receipt(t *testing.T, w *httptest.ResponseRecorder) admin.Receipt {
	t.Helper()
	var out struct {
		Data admin.Receipt `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Data
}

func TestT15_02And03HTTPAcceptanceAndLostReplyRetryReturnSameDurableTask(t *testing.T) {
	r, d, req := taskRepository(t)
	d.initialized = true
	h := management(t, r, req.AcceptedAt, admin.ForceFull)
	first := httptest.NewRecorder()
	h.ServeHTTP(first, managementRequest(t.Context()))
	one := receipt(t, first)
	if first.Code != 202 || !one.Created || one.Status != "accepted" || one.TaskID == "" || len(d.records) != 1 || d.revision != 0 || d.owner.TaskID != "" {
		t.Fatal(first.Code, one)
	}
	row := d.records["request-key"]
	if row[4] != "operator-1" || row[7] != nil {
		t.Fatal("untrusted identity or premature start")
	}
	// The first response is discarded. Retrying while the worker is running
	// must resolve the idempotency key before considering the busy condition.
	launch, err := r.StartAccepted(t.Context(), one.TaskID, "worker", req.AcceptedAt.Add(time.Minute))
	if err != nil || !launch.Started {
		t.Fatal(launch, err)
	}
	second := httptest.NewRecorder()
	h.ServeHTTP(second, managementRequest(t.Context()))
	two := receipt(t, second)
	if second.Code != 202 || two.Created || two.TaskID != one.TaskID || two.Status != "running" || len(d.records) != 1 || d.revision != 1 {
		t.Fatal(second.Code, two)
	}
}

func TestManualIncrementalAdmissionAndReplay(t *testing.T) {
	r, d, req := taskRepository(t)
	h := management(t, r, req.AcceptedAt, admin.Incremental)
	blocked := httptest.NewRecorder()
	h.ServeHTTP(blocked, managementRequest(t.Context()))
	if blocked.Code != 409 || len(d.records) != 0 {
		t.Fatal("incremental admitted before initialization", blocked.Code)
	}
	d.initialized = true
	first := httptest.NewRecorder()
	h.ServeHTTP(first, managementRequest(t.Context()))
	one := receipt(t, first)
	if first.Code != 202 || !one.Created || d.records["request-key"][2] != "incremental" || d.records["request-key"][4] != "operator-1" {
		t.Fatal("wrong manual task", first.Code, one)
	}
	if _, err := r.StartAccepted(t.Context(), one.TaskID, "worker", req.AcceptedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	replay := httptest.NewRecorder()
	h.ServeHTTP(replay, managementRequest(t.Context()))
	two := receipt(t, replay)
	if replay.Code != 202 || two.Created || two.TaskID != one.TaskID || len(d.records) != 1 {
		t.Fatal("lost response duplicated task", replay.Code, two)
	}
	other := managementRequest(t.Context())
	other.Header.Set("Idempotency-Key", "new-sync")
	busy := httptest.NewRecorder()
	h.ServeHTTP(busy, other)
	if busy.Code != 409 || len(d.records) != 1 {
		t.Fatal("overlapping task admitted", busy.Code)
	}
	conflict := httptest.NewRecorder()
	management(t, r, req.AcceptedAt, admin.ForceFull).ServeHTTP(conflict, managementRequest(t.Context()))
	if conflict.Code != 409 || len(d.records) != 1 {
		t.Fatal("incremental key changed into full update", conflict.Code)
	}
}

func TestT15_04And05And13AcceptanceFailuresCreateNoTask(t *testing.T) {
	for _, failure := range []string{"commit", "pending", "owner", "unfinished", "uninitialized-full"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			d.initialized = true
			action := admin.ForceFull
			expected := 409
			before := 0
			switch failure {
			case "commit":
				d.failCommit = true
				expected = 503
			case "pending":
				d.pending = true
			case "owner":
				d.owner = control.Ownership{TaskID: "existing", OwnerID: "worker", Generation: 1}
			case "unfinished":
				if _, err := r.Accept(t.Context(), req); err != nil {
					t.Fatal(err)
				}
				before = 1
			case "uninitialized-full":
				d.initialized = false
			}
			h := management(t, r, req.AcceptedAt, action)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, managementRequest(t.Context()))
			if w.Code != expected || len(d.records) != before || d.revision != 0 || strings.Contains(w.Body.String(), "injected") {
				t.Fatal(w.Code, w.Body.String())
			}
			if strings.HasPrefix(failure, "uninitialized") && !strings.Contains(w.Body.String(), "initialize_required") {
				t.Fatal("missing initialization guidance")
			}
		})
	}
}

type canceledAdmission struct {
	r      control.Repository
	cancel context.CancelFunc
}

func (c canceledAdmission) Admit(ctx context.Context, req control.Request) (control.Acceptance, error) {
	out, err := c.r.Admit(ctx, req)
	if err == nil {
		c.cancel()
	}
	return out, err
}
func TestT15_06ClientDisconnectAfterCommitDoesNotCancelPersistedWorkerTask(t *testing.T) {
	r, d, req := taskRepository(t)
	client, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.initialized = true
	h := management(t, canceledAdmission{r: r, cancel: cancel}, req.AcceptedAt, admin.ForceFull)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, managementRequest(client))
	accepted := receipt(t, w)
	if client.Err() == nil || w.Code != 202 {
		t.Fatal("disconnect not injected")
	}
	launch, err := r.StartAccepted(t.Context(), accepted.TaskID, "worker", req.AcceptedAt.Add(time.Minute))
	if err != nil || !launch.Started || d.records["request-key"][3] != "running" {
		t.Fatal(launch, err)
	}
	again, err := r.StartAccepted(t.Context(), accepted.TaskID, "worker", req.AcceptedAt.Add(time.Hour))
	if err != nil || again.Started || d.revision != 1 {
		t.Fatal("resume reset schedule", again, err)
	}
}

func TestT15_05ConcurrentAdmissionsAndWorkerStartRemainExclusive(t *testing.T) {
	r, d, req := taskRepository(t)
	req.Mode = "initialize"
	other := nextRequest(req, req.AcceptedAt)
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)
	for _, request := range []control.Request{req, other} {
		go func() { defer wg.Done(); _, err := r.Admit(t.Context(), request); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, control.ErrBusy) {
			t.Fatal(err)
		}
	}
	if success != 1 || len(d.records) != 1 || d.revision != 0 {
		t.Fatal("multiple admissions")
	}
}

func TestT15_04WorkerStartCommitFailureAndCooldownPreserveAcceptance(t *testing.T) {
	for _, failure := range []string{"commit", "cooldown", "pending"} {
		t.Run(failure, func(t *testing.T) {
			r, d, req := taskRepository(t)
			req.Mode = "initialize"
			if _, err := r.Admit(t.Context(), req); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "commit":
				d.failCommit = true
			case "cooldown":
				at := req.AcceptedAt.Add(time.Hour)
				d.cooldown = &at
			case "pending":
				d.pending = true
			}
			if out, err := r.StartAccepted(t.Context(), req.ID, "worker", req.AcceptedAt); err == nil || out.Started || d.owner.TaskID != "" || d.records[req.IdempotencyKey][3] != "accepted" || d.revision != 0 {
				t.Fatal(out, err)
			}
		})
	}
}

func TestT15_02EachManualModeRequiresItsInitializationState(t *testing.T) {
	for _, mode := range []string{"initialize", "incremental", "force_full"} {
		t.Run(mode, func(t *testing.T) {
			r, d, req := taskRepository(t)
			req.Mode = mode
			d.initialized = mode != "initialize"
			out, err := r.Admit(t.Context(), req)
			if err != nil || !out.Created || out.Task.Mode != mode || out.Task.StartedAt != nil {
				t.Fatal(out, err)
			}
			req.RequestedBy = "other-operator"
			if _, err = r.Admit(t.Context(), req); !errors.Is(err, control.ErrIdempotency) {
				t.Fatal("idempotency disclosed another operator's task", err)
			}
		})
	}
}
