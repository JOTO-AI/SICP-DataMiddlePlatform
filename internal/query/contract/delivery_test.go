package contract_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func TestT25_01DeliveryExamplesParseAndKeepFullResponseFields(t *testing.T) {
	raw, err := os.ReadFile("../../../development_docs/examples/phase2_queries.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Operation      contract.Operation
		Scenario, Path string
		Request        struct {
			Headers map[string]string
			Body    json.RawMessage
		}
		Response struct {
			Status  int
			Headers map[string]string
			Body    struct {
				Code, Message string
				RequestID     string `json:"request_id"`
				Data          json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 20 {
		t.Fatal("five operations need four scenarios each")
	}
	seen := map[string]bool{}
	paths := map[contract.Operation]string{contract.Organizations: "organizations", contract.Students: "students", contract.Staff: "staff", contract.StaffTeachingClasses: "staff/teaching-classes", contract.TeachingClassStudents: "teaching-classes/students"}
	for _, tc := range cases {
		key := string(tc.Operation) + "/" + tc.Scenario
		if seen[key] {
			t.Fatal("duplicate scenario")
		}
		seen[key] = true
		path, ok := paths[tc.Operation]
		if !ok || tc.Path != "/api/v1/sicp/"+path+"/query" {
			t.Fatal("wrong path")
		}
		if tc.Request.Headers["X-API-Key"] != "<QUERY_API_KEY>" || tc.Response.Body.RequestID != "<SERVER_REQUEST_ID>" {
			t.Fatal("placeholder required")
		}
		_, err := transport.Parse(tc.Operation, bytes.NewReader(tc.Request.Body))
		if tc.Scenario == "error" {
			if err == nil || tc.Response.Status != 400 || tc.Response.Body.Code != "invalid_argument" || string(tc.Response.Body.Data) != "null" || len(tc.Response.Headers) != 0 {
				t.Fatal(key, "failure contract")
			}
			continue
		}
		if err != nil || tc.Response.Status != 200 || tc.Response.Body.Code != "ok" || tc.Response.Headers["X-Query-Snapshot"] == "" || tc.Response.Headers["X-SICP-Combination"] == "" {
			t.Fatal(key, err)
		}
		if tc.Scenario == "next" && tc.Request.Headers["X-Query-Snapshot"] == "" {
			t.Fatal("continuation needs same-query token")
		}
		var data struct {
			Total int
			Items []json.RawMessage
		}
		if err := json.Unmarshal(tc.Response.Body.Data, &data); err != nil {
			t.Fatal(err)
		}
		if tc.Scenario == "empty" && (data.Total != 0 || data.Items == nil || len(data.Items) != 0) {
			t.Fatal("empty is []")
		}
		if (tc.Scenario == "first" || tc.Scenario == "next") && (data.Total != 2 || len(data.Items) != 1) {
			t.Fatal("page counts")
		}
		var target any
		switch tc.Operation {
		case contract.Organizations:
			target = &contract.OrganizationsResponse{}
		case contract.Students:
			target = &contract.StudentsResponse{}
		case contract.Staff:
			target = &contract.StaffResponse{}
		case contract.StaffTeachingClasses:
			target = &contract.StaffTeachingClassesResponse{}
		case contract.TeachingClassStudents:
			target = &contract.TeachingClassStudentsResponse{}
		}
		if err := json.Unmarshal(tc.Response.Body.Data, target); err != nil {
			t.Fatal(err)
		}
		assertPhase2JSON(t, target, tc.Response.Body.Data)
	}
	for op := range paths {
		for _, scenario := range []string{"first", "next", "empty", "error"} {
			if !seen[string(op)+"/"+scenario] {
				t.Fatal("missing scenario")
			}
		}
	}
}
