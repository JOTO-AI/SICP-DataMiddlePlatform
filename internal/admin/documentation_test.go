package admin

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/logquery"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/operations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type documentedExample struct {
	Name, Pattern, Path string
	Request             json.RawMessage
	RequestHeaders      map[string]string `json:"request_headers"`
	Status              int
	ResponseType        string `json:"response_type"`
	ResponseScope       string `json:"response_scope"`
	Frame               struct {
		Event, ID string
		Data      json.RawMessage
	}
	Response struct {
		Code, Message string
		Data          json.RawMessage
	}
}
type documentedContract struct {
	CatalogVersion string `json:"catalog_version"`
	Examples       []documentedExample
	Business       []struct {
		Operation contract.Operation
		Path      string
		Request   json.RawMessage
	}
	Errors []struct {
		Status int
		Code   string
		Data   json.RawMessage
	}
}

func docRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func documentation(t *testing.T) documentedContract {
	t.Helper()
	var d documentedContract
	if err := json.Unmarshal(docRead(t, "development_docs/verification/operations_http_examples.json"), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func docDecode(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := logquery.DecodeRequest(bytes.NewReader(raw), out); err != nil {
		t.Fatal("documented JSON violates DTO", string(raw), err)
	}
}
func TestT35_04DocumentedRequestsUseActualParsers(t *testing.T) {
	d := documentation(t)
	if d.CatalogVersion != operations.CatalogVersion {
		t.Fatal("stale catalog version")
	}
	for _, e := range d.Examples {
		t.Run(e.Name, func(t *testing.T) {
			u, err := url.Parse(e.Path)
			if err != nil {
				t.Fatal(err)
			}
			switch e.Name {
			case "log_live", "log_history":
				filter, _, err := logquery.ParseFilter(u.Query(), time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				var page map[string]json.RawMessage
				if err := json.Unmarshal(e.Response.Data, &page); err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(filter)
				if err != nil {
					t.Fatal(err)
				}
				var actual, documented any
				if json.Unmarshal(raw, &actual) != nil || json.Unmarshal(page["filter"], &documented) != nil || !reflect.DeepEqual(actual, documented) {
					t.Fatal("example filter differs from normalized request")
				}
			case "source_test":
				var v struct {
					operations.TestRequest
					Key string `json:"idempotency_key"`
				}
				docDecode(t, e.Request, &v)
				if v.Normalize() != nil || !operations.ValidID(v.Key) {
					t.Fatal("invalid documented source request")
				}
				bad := bytes.TrimSuffix(e.Request, []byte("}"))
				bad = append(bad, []byte(`,"operator":"forged"}`)...)
				if logquery.DecodeRequest(bytes.NewReader(bad), &v) == nil {
					t.Fatal("source override accepted")
				}
			case "save_notifications":
				var v operations.NotificationSettings
				docDecode(t, e.Request, &v)
				if v.Normalize(false) != nil {
					t.Fatal("invalid saved settings")
				}
			case "save_retention":
				var v operations.Retention
				docDecode(t, e.Request, &v)
				if !v.Valid() {
					t.Fatal("invalid retention")
				}
			case "test_notifications":
				var v struct {
					Revision int64  `json:"revision,string"`
					Key      string `json:"idempotency_key"`
				}
				docDecode(t, e.Request, &v)
				if v.Revision < 1 || !operations.ValidID(v.Key) {
					t.Fatal("invalid explicit test")
				}
			case "preview":
				var v struct {
					Source storage.LogSource `json:"source"`
				}
				docDecode(t, e.Request, &v)
				if v.Source != storage.PlatformLog && v.Source != storage.UpstreamLog {
					t.Fatal("invalid source")
				}
			case "cleanup":
				var v struct {
					operations.CleanupRequest
					Key string `json:"idempotency_key"`
				}
				docDecode(t, e.Request, &v)
				if !operations.ValidID(v.PreviewID) || !operations.ValidID(v.Key) || v.Revision < 1 {
					t.Fatal("invalid cleanup")
				}
			case "incremental", "full", "table_full", "rerun":
				r := httptest.NewRequest("POST", e.Path, bytes.NewReader(e.Request))
				for k, v := range e.RequestHeaders {
					r.Header.Set(k, v)
				}
				if _, ok := requestKey(r); !ok || !emptyBody(httptest.NewRecorder(), r) {
					t.Fatal("invalid legacy task request")
				}
			}
		})
	}
	if len(d.Business) != len(contract.Definitions()) {
		t.Fatal("incomplete nine-interface examples")
	}
	seen := map[contract.Operation]bool{}
	for _, e := range d.Business {
		if seen[e.Operation] {
			t.Fatal("duplicate business operation")
		}
		seen[e.Operation] = true
		if _, err := transport.Parse(e.Operation, bytes.NewReader(e.Request)); err != nil {
			t.Fatal(e.Operation, err)
		}
		api, ok := operations.LookupAPI(string(e.Operation))
		if !ok || e.Path != "/console-api/query"+api.Path || api.SuccessExample == nil || api.FailureExample == nil {
			t.Fatal("business response/example reference missing")
		}
	}
}
func TestT35_04ResponseShapesNullsAndRoutesStayDocumented(t *testing.T) {
	d := documentation(t)
	patterns := map[string]bool{}
	for _, e := range d.Examples {
		patterns[e.Pattern] = true
		if e.ResponseScope == "sse_event_data" {
			if e.Frame.Event != "event" || e.Frame.ID == "" {
				t.Fatal("missing SSE frame identity")
			}
			var item logquery.EventItem
			docDecode(t, e.Frame.Data, &item)
			continue
		}
		if e.Response.Code != "ok" && !(e.Status == 202 && e.Response.Code == "accepted") {
			t.Fatal(e.Name, "bad envelope")
		}
		var target any
		switch e.ResponseType {
		case "session":
			target = &struct{ Operator, CSRF string }{}
		case "null":
			if string(e.Response.Data) != "null" {
				t.Fatal("logout not null")
			}
			continue
		case "log_page":
			target = &logquery.Page{}
		case "log_detail":
			target = &logquery.Detail{}
		case "log_event":
			target = &logquery.EventItem{}
		case "event_page":
			target = &operations.EventPage{}
		case "task":
			target = &TaskView{}
		case "operation_page":
			target = &struct {
				Items  []operations.Operation
				Next   *string `json:"next_cursor"`
				Parent string  `json:"parent_operation_id"`
			}{}
		case "sources":
			target = &struct{ Items []operations.SourceState }{}
		case "overview":
			target = &operations.Overview{}
		case "catalog":
			target = &struct {
				Version string
				Items   []operations.API
			}{}
		case "api":
			target = &operations.API{}
		case "job":
			target = &operations.Job{}
		case "test_result":
			target = &struct {
				Job    operations.Job
				State  string `json:"result_state"`
				Result json.RawMessage
			}{}
		case "notification_settings_view":
			target = &struct {
				Settings    operations.NotificationSettings
				Channel     string                        `json:"channel_state"`
				Diagnostics operations.ChannelDiagnostics `json:"channel_diagnostics"`
			}{}
		case "notification_settings":
			target = &operations.NotificationSettings{}
		case "decisions":
			target = &struct {
				Items []operations.NotificationDecision
				Next  *string `json:"next_cursor"`
			}{}
		case "deliveries":
			target = &struct {
				Items []operations.Delivery
				Next  *string `json:"next_cursor"`
			}{}
		case "delivery_detail":
			target = &struct {
				Delivery operations.Delivery
				Attempts []operations.DeliveryAttempt
			}{}
		case "retention":
			target = &operations.Retention{}
		case "preview":
			target = &operations.CleanupPreview{}
		case "receipt":
			target = &Receipt{}
		case "system":
			if !strings.Contains(string(e.Response.Data), `"retention": null`) || !strings.Contains(string(e.Response.Data), `"durable_reference_retirement"`) {
				t.Fatal("system excerpt hides unknown/protection")
			}
			continue
		default:
			t.Fatal("unreviewed response type", e.ResponseType)
		}
		t.Run(e.Name, func(t *testing.T) { docDecode(t, e.Response.Data, target) })
		if v, ok := target.(*struct{ Items []operations.SourceState }); ok {
			for _, item := range v.Items {
				found := false
				for _, table := range source.Catalog() {
					found = found || item.ID == table.Name && item.PrimaryKey == table.PrimaryKey
				}
				if !found {
					t.Fatal("stale source primary key")
				}
			}
		}
	}
	// Route additions must bring their own example, not just an updated count.
	routeRE := regexp.MustCompile(`"((?:GET|PUT|POST) /admin/v1/[^"\n]+)"`)
	for _, file := range []string{"operations.go", "api_catalog.go", "notifications.go", "maintenance.go", "routes.go"} {
		for _, m := range routeRE.FindAllSubmatch(docRead(t, "internal/admin/"+file), -1) {
			if !patterns[string(m[1])] {
				t.Fatal("undocumented route", string(m[1]))
			}
		}
	}
	for _, pattern := range []string{"GET /admin/v1/logs/{source}/events", "GET /admin/v1/logs/{source}/events/{event_id}", "GET /admin/v1/logs/{source}/stream"} {
		if !patterns[pattern] {
			t.Fatal("missing log route")
		}
	}
	known := map[string]error{"invalid_argument": logquery.ErrArgument, "forbidden": logquery.ErrForbidden, "not_found": logquery.ErrNotFound, "cursor_expired": logquery.ErrExpired, "request_too_large": logquery.ErrTooLarge, "resource_exhausted": logquery.ErrLimit, "proxy_unavailable": logquery.ErrProxy, "unavailable": logquery.ErrUnavailable, "deadline_exceeded": logquery.ErrTimeout}
	for _, e := range d.Errors {
		w := httptest.NewRecorder()
		if e.Code == "revision_conflict" {
			operationError(w, operations.ErrConflict)
		} else if e.Code == "unauthenticated" {
			(&Sessions{sessions: map[[32]byte]session{}, now: time.Now}).Current(w, httptest.NewRequest("GET", "/console-api/session", nil))
		} else {
			err, ok := known[e.Code]
			if !ok {
				t.Fatal("unknown documented error", e.Code)
			}
			logFailure(w, err)
		}
		var out struct {
			Code string
			Data json.RawMessage
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || w.Code != e.Status || out.Code != e.Code || string(out.Data) != "null" || string(e.Data) != "null" {
			t.Fatal("error contract drift", e.Code)
		}
	}
}
