package metadata_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type phase2ResponseCase struct {
	op      contract.Operation
	fixture string
	target  any
	empty   any
	tables  []string
}

func phase2Responses() []phase2ResponseCase {
	return []phase2ResponseCase{
		{contract.Organizations, "organizations_response", &contract.OrganizationsResponse{}, contract.OrganizationsResponse{}, []string{"dwd_yxsdwjbsjzlb"}},
		{contract.Students, "students_response", &contract.StudentsResponse{}, contract.StudentsResponse{}, []string{"dwd_bzksjbsjzlb", "dwd_bzksxjjbsjzlb", "dwd_bzksgrtxsjzlb", "dwd_yxsdwjbsjzlb", "dwd_xqjbsjzlb", "dwd_bzkszyxxsjzlb", "dwd_bzksbjsjzlb", "dwd_jzgjcsjzlb"}},
		{contract.Staff, "staff_response", &contract.StaffResponse{}, contract.StaffResponse{}, []string{"dwd_jzgjcsjzlb", "dwd_grtxfszlb", "dwd_yxsdwjbsjzlb"}},
		{contract.StaffTeachingClasses, "staff_teaching_classes_response", &contract.StaffTeachingClassesResponse{}, contract.StaffTeachingClassesResponse{}, []string{"dwd_bzkspksjzlb", "dwd_bzkskcsjzlb", "dwd_jzgjcsjzlb", "dwd_bzkskbsjzlb"}},
		{contract.TeachingClassStudents, "teaching_class_students_response", &contract.TeachingClassStudentsResponse{}, contract.TeachingClassStudentsResponse{}, []string{"dwd_bzkspksjzlb", "dwd_bzkskbsjzlb", "dwd_bzksjbsjzlb", "dwd_bzksxjjbsjzlb", "dwd_yxsdwjbsjzlb", "dwd_bzksbjsjzlb"}},
	}
}

func TestT17_02Phase2CompleteDependenciesEvenForEmptyResults(t *testing.T) {
	knownTables := map[string]bool{}
	for _, table := range source.Catalog() {
		knownTables[table.Name] = true
	}
	at := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	times, err := compute.NewTimes("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range phase2Responses() {
		t.Run(string(tc.op), func(t *testing.T) {
			deps, err := metadata.Dependencies(tc.op)
			if err != nil || !reflect.DeepEqual(deps, tc.tables) {
				t.Fatal(deps, err)
			}
			deps[0] = "mutated"
			again, _ := metadata.Dependencies(tc.op)
			if !reflect.DeepEqual(again, tc.tables) {
				t.Fatal("shared dependency slice")
			}
			f := &facts{checks: map[string]time.Time{"unrelated": at.Add(-time.Hour)}}
			for i, table := range tc.tables {
				if !knownTables[table] {
					t.Fatal("table outside source catalog", table)
				}
				f.checks[table] = at.Add(time.Duration(i) * time.Minute)
			}
			provider := metadata.Provider{Store: f}
			got, err := provider.SyncedAt(t.Context(), handle(), tc.op)
			if err != nil || !got.Equal(at) || f.bundle != handle().Combination {
				t.Fatal(got, err)
			}
			payload, err := metadata.Payload(tc.empty)
			if err != nil {
				t.Fatal(err)
			}
			result, err := metadata.Compose(tc.op, payload, got, times)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(result)
			if err != nil || !strings.Contains(string(encoded), `"items":[]`) || !strings.Contains(string(encoded), `"synced_at":"2026-10-12T09:00:00+08:00"`) {
				t.Fatal(string(encoded), err)
			}
			for _, table := range tc.tables {
				saved := f.checks[table]
				delete(f.checks, table)
				if _, err := provider.SyncedAt(t.Context(), handle(), tc.op); !errors.Is(err, metadata.ErrMetadata) {
					t.Fatal("missing dependency accepted", table, err)
				}
				f.checks[table] = time.Time{}
				if _, err := provider.SyncedAt(t.Context(), handle(), tc.op); !errors.Is(err, metadata.ErrMetadata) {
					t.Fatal("unconfirmed dependency accepted", table, err)
				}
				f.checks[table] = saved
			}
			f.err = errors.New("metadata unavailable")
			if _, err := provider.SyncedAt(t.Context(), handle(), tc.op); !errors.Is(err, f.err) {
				t.Fatal(err)
			}
			f.err = nil
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := provider.SyncedAt(ctx, handle(), tc.op); !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}

func TestT17_03Phase2PayloadPreservesFieldsAndRefreshesSyncedAt(t *testing.T) {
	data, err := os.ReadFile("../contract/testdata/phase2.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]json.RawMessage
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	times, err := compute.NewTimes("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	for _, tc := range phase2Responses() {
		t.Run(string(tc.op), func(t *testing.T) {
			if err := json.Unmarshal(fixtures[tc.fixture], tc.target); err != nil {
				t.Fatal(err)
			}
			payload, err := metadata.Payload(reflect.ValueOf(tc.target).Elem().Interface())
			if err != nil || strings.Contains(string(payload), "synced_at") {
				t.Fatal(string(payload), err)
			}
			var expected map[string]any
			if err := json.Unmarshal(fixtures[tc.fixture], &expected); err != nil {
				t.Fatal(err)
			}
			for _, instant := range []time.Time{at, at.Add(time.Hour)} {
				response, err := metadata.Compose(tc.op, payload, instant, times)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				var actual map[string]any
				if err := json.Unmarshal(encoded, &actual); err != nil {
					t.Fatal(err)
				}
				expected["synced_at"] = instant.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(time.RFC3339)
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("payload changed business data: %s", encoded)
				}
				again, err := metadata.Payload(reflect.ValueOf(response).Elem().Interface())
				if err != nil || string(again) != string(payload) {
					t.Fatal("fresh check time leaked into cached business data", err)
				}
			}
			for _, bad := range []string{string(fixtures[tc.fixture]), `{}`, `{"total":0,"items":null}`, `{"total":null,"items":[]}`, `{"total":0,"items":[],"unknown":true}`} {
				if _, err := metadata.Compose(tc.op, []byte(bad), at, times); err == nil {
					t.Fatal("invalid cached envelope accepted", bad)
				}
			}
		})
	}
}
