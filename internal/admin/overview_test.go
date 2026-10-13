package admin

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type overviewFixture struct {
	treeViewDB
	active                   control.ActiveMetadata
	initialized, found, open bool
	failure                  string
	at                       time.Time
}

func (f *overviewFixture) RecentTasks(context.Context) (control.TaskOverview, error) {
	if f.failure == "tasks" {
		return control.TaskOverview{}, errors.New("private tasks")
	}
	return control.TaskOverview{Initialized: f.initialized, Items: []control.TaskSummary{{ID: "task", AcceptedAt: f.at, PublishedAt: &f.at, CheckedAt: &f.at}}, CooldownUntil: &f.at}, nil
}
func (f *overviewFixture) Read(context.Context) (control.ActiveMetadata, bool, error) {
	if f.failure == "active" {
		return f.active, true, errors.New("private active")
	}
	return f.active, f.found, nil
}

type overviewRow func(...any) error

func (r overviewRow) Scan(v ...any) error { return r(v...) }
func (f *overviewFixture) Begin(context.Context, storage.Connection) (storage.Transaction, error) {
	return f, nil
}
func (f *overviewFixture) QueryRow(context.Context, string, ...any) storage.Row {
	return overviewRow(func(v ...any) error {
		if f.failure == "actions" {
			return errors.New("private actions")
		}
		*v[0].(*bool) = f.initialized
		return nil
	})
}
func (f *overviewFixture) Query(context.Context, string, ...any) (storage.Rows, error) {
	if f.failure == "slots" {
		return nil, errors.New("private slots")
	}
	return &overviewRows{f: f}, nil
}

type overviewRows struct {
	f   *overviewFixture
	pos int
}

func (r *overviewRows) Next() bool { r.pos++; return r.pos <= 1 }
func (*overviewRows) Close()       {}
func (*overviewRows) Err() error   { return nil }
func (r *overviewRows) Scan(v ...any) error {
	bundle := "bundle"
	*v[0].(*string) = "M"
	*v[1].(*int16) = 1
	*v[2].(*int64) = 7
	*v[3].(**string) = &bundle
	*v[4].(*bool) = r.f.open
	*v[5].(*string) = "ready"
	return nil
}
func TestT31_10OverviewSeparatesDependencyReadinessAndTaskTimes(t *testing.T) {
	for _, tc := range []struct {
		name, state              string
		initialized, found, open bool
		failure                  string
		code                     int
	}{
		{"ready", "available", true, true, true, "", 200},
		{"closed_slot", "slot_unavailable", true, true, false, "", 200},
		{"mismatched_slot", "slot_unavailable", true, true, true, "mismatch", 200},
		{"empty_initialized", "empty", true, false, true, "", 200},
		{"first_start", "empty", false, false, true, "", 200},
		{"active_failure", "unavailable", true, true, true, "active", 200},
		{"invalid_active", "unavailable", true, true, true, "invalid", 200},
		{"tasks_failure", "", true, true, true, "tasks", 503},
		{"slots_failure", "", true, true, true, "slots", 503},
		{"actions_failure", "", true, true, true, "actions", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &overviewFixture{initialized: tc.initialized, found: tc.found, open: tc.open, failure: tc.failure, at: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)}
			f.active = control.ActiveMetadata{Sequence: 1, EventID: "publish", TaskID: "task", OwnerGeneration: 1, Position: control.SlotIdentity{Position: storage.Position{Instance: storage.Main, Slot: 1}, Generation: 7, BundleID: "bundle"}}
			if tc.failure == "mismatch" {
				f.active.Position.Generation++
			}
			if tc.failure == "invalid" {
				f.active.Sequence = 0
			}
			auth, err := NewSessions("admin", "synthetic-password", "query", false)
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			views := OperationsViews{Control: f, Repository: operations.Repository{Database: f}, Active: f}
			if err = views.Mount(mux, auth, StatusHandler{}); err != nil {
				t.Fatal(err)
			}
			cookie, _ := loginSession(t, auth)
			req := httptest.NewRequest("GET", "/admin/v1/overview", nil)
			req.AddCookie(cookie)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.code || strings.Contains(w.Body.String(), "private") {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.code != 200 {
				if !strings.Contains(w.Body.String(), `"data":null`) {
					t.Fatal("partial overview")
				}
				return
			}
			var got struct {
				Data operations.Overview `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &got) != nil {
				t.Fatal("invalid JSON")
			}
			v := got.Data
			if v.ActiveState != tc.state || len(v.Tasks.Items) != 1 || v.Tasks.Items[0].FinishedAt != nil || v.Tasks.Items[0].PublishedAt == nil || !v.Tasks.Items[0].PublishedAt.Equal(f.at) || v.Tasks.Items[0].CheckedAt == nil || v.Tasks.CooldownUntil == nil {
				t.Fatal("invented or lost state", v)
			}
			if tc.state == "available" && v.Actions["force_full"] != "available_recheck_on_submit" || tc.state != "available" && v.Actions["force_full"] == "available_recheck_on_submit" {
				t.Fatal("incorrect admission guidance", v.Actions)
			}
			if (tc.state == "empty" || tc.state == "unavailable") && v.Active != nil {
				t.Fatal("unknown position became normal")
			}
		})
	}
}
