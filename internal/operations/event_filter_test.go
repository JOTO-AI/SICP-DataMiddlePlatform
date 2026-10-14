package operations

import (
	"encoding/json"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func capturedUpdateTask(t *testing.T) Event {
	t.Helper()
	raw, e := os.ReadFile("../../development_docs/verification/operations_http_examples.json")
	if e != nil {
		t.Fatal(e)
	}
	var doc struct {
		Examples []struct {
			Name     string
			Response struct{ Data json.RawMessage }
		}
	}
	if e = json.Unmarshal(raw, &doc); e != nil {
		t.Fatal(e)
	}
	for _, v := range doc.Examples {
		if v.Name == "update_detail" {
			var task struct {
				TaskID     string `json:"task_id"`
				Mode       string
				Status     string
				AcceptedAt time.Time `json:"accepted_at"`
			}
			if e = json.Unmarshal(v.Response.Data, &task); e != nil {
				t.Fatal(e)
			}
			return Event{ID: task.TaskID, TaskID: task.TaskID, Mode: task.Mode, Status: task.Status, AcceptedAt: task.AcceptedAt}
		}
	}
	t.Fatal("missing recorded task")
	return Event{}
}
func TestFrontendUpdateFilterDefaultsAndStrictInputs(t *testing.T) {
	at := capturedUpdateTask(t).AcceptedAt.Add(time.Hour)
	for _, values := range []url.Values{
		{"unknown": {"x"}}, {"mode": {"incremental", "force_full"}}, {"mode": {"unknown"}}, {"status": {"bad status"}}, {"task_id": {"../other"}}, {"from": {"not-a-time"}}, {"from": {at.Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano)}}, {"from": {at.Add(time.Hour).Format(time.RFC3339Nano)}},
	} {
		p := Token{At: at}
		if _, e := BindEventFilter(&p, values); e != ErrArgument {
			t.Fatalf("accepted invalid filter %v: %v", values, e)
		}
	}
	p := Token{At: at}
	f, e := BindEventFilter(&p, url.Values{})
	if e != nil || !f.From.Equal(at.Add(-24*time.Hour)) || !f.To.Equal(at) {
		t.Fatal(f, e)
	}
}
func TestFrontendUpdateFilterSignedContinuationCannotChangeScope(t *testing.T) {
	task := capturedUpdateTask(t)
	at := task.AcceptedAt.Add(time.Hour)
	p := Token{At: at, Expires: at.Add(time.Hour), Size: 50, Scope: "update-events", Subject: "synthetic-admin"}
	original, e := BindEventFilter(&p, url.Values{"mode": {task.Mode}, "status": {task.Status}, "task_id": {task.ID}})
	if e != nil {
		t.Fatal(e)
	}
	tokens := NewTokens(strings.Repeat("f", 32))
	raw, e := tokens.Encode(p)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := tokens.Decode(raw, p.Scope, p.Subject, at.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	got, e := BindEventFilter(&decoded, url.Values{"cursor": {raw}})
	if e != nil || got != original {
		t.Fatal("lost filter", got, e)
	}
	for _, values := range []url.Values{{"cursor": {raw}, "mode": {"force_full"}}, {"cursor": {raw}, "status": {"failed"}}, {"cursor": {raw}, "task_id": {"another-task"}}, {"cursor": {raw}, "to": {at.Add(time.Minute).Format(time.RFC3339)}}} {
		q := decoded
		if _, e := BindEventFilter(&q, values); e != ErrArgument {
			t.Fatal("cursor changed filters", e)
		}
	}
	legacy := Token{At: at}
	if _, e := BindEventFilter(&legacy, url.Values{"cursor": {"legacy"}}); e != ErrConflict {
		t.Fatal(e)
	}
	if _, e := tokens.Decode(raw, p.Scope, "other-actor", at.Add(time.Second)); e != ErrForbidden {
		t.Fatal(e)
	}
}
func TestFrontendUpdateFilterRepositoryBindsRecordedTaskAndTime(t *testing.T) {
	task := capturedUpdateTask(t)
	f := EventFilter{From: task.AcceptedAt.Add(-time.Hour), To: task.AcceptedAt.Add(time.Hour), Mode: task.Mode, Status: task.Status, TaskID: task.ID}
	rows := &viewRows{items: [][]any{{task.ID, task.Mode, task.Status, nil, task.AcceptedAt, nil, nil, nil, nil, nil}}}
	tx := &viewTx{query: func(q string, a []any) (storage.Rows, error) {
		if len(a) != 8 || a[3] != f.From || a[4] != f.To || a[5] != f.Mode || a[6] != f.Status || a[7] != task.ID || !strings.Contains(q, "accepted_at>=$4 AND accepted_at<$5") || strings.Contains(q, task.ID) {
			t.Fatal("filter not bound", q, a)
		}
		return rows, nil
	}}
	got, e := (Repository{Database: viewDB{tx}}).Events(t.Context(), f.To, "", 51, f)
	if e != nil || len(got) != 1 || got[0].ID != task.ID || got[0].StartedAt != nil || !rows.closed {
		t.Fatal(got, e)
	}
}
