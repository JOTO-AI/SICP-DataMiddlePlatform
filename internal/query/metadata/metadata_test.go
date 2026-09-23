package metadata_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type facts struct {
	checks map[string]time.Time
	bundle string
	err    error
}

func (f *facts) BundleChecks(_ context.Context, bundle string) (map[string]time.Time, error) {
	f.bundle = bundle
	return f.checks, f.err
}
func handle() read.Handle {
	return read.Handle{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 2, Combination: "old-combination"}
}

func TestT11_17CachedBusinessDataUsesNewConfirmedCheckWithoutChangingSourceTime(t *testing.T) {
	at := time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC)
	f := &facts{checks: map[string]time.Time{}}
	deps, _ := metadata.Dependencies(contract.StudentGrades)
	for _, table := range deps {
		f.checks[table] = at
	}
	provider := metadata.Provider{Store: f}
	sourceTime := "2025-01-01T00:00:00+08:00"
	payload, err := metadata.Payload(contract.GradesResponse{Total: 1, SyncedAt: "stale", Items: contract.Array[contract.Grade]{{GradeID: "001", SourceUpdatedAt: &sourceTime}}})
	if err != nil || strings.Contains(string(payload), "synced_at") {
		t.Fatal(string(payload), err)
	}
	times, _ := compute.NewTimes("Asia/Shanghai")
	old, err := provider.SyncedAt(t.Context(), handle(), contract.StudentGrades)
	if err != nil || !old.Equal(at) || f.bundle != "old-combination" {
		t.Fatal(old, f.bundle, err)
	}
	for _, table := range deps {
		f.checks[table] = at.Add(time.Hour)
	}
	current, err := provider.SyncedAt(t.Context(), handle(), contract.StudentGrades)
	if err != nil {
		t.Fatal(err)
	}
	value, err := metadata.Compose(contract.StudentGrades, payload, current, times)
	if err != nil {
		t.Fatal(err)
	}
	response := value.(*contract.GradesResponse)
	if response.SyncedAt != "2026-09-18T10:00:00+08:00" || response.Total != 1 || *response.Items[0].SourceUpdatedAt != sourceTime {
		t.Fatal(response)
	}
	again, err := metadata.Payload(*response)
	if err != nil || string(again) != string(payload) {
		t.Fatal(string(again), err)
	}
}

func TestT11_18EveryOperationUsesItsCompleteFixedDependencyMinimumEvenWhenEmpty(t *testing.T) {
	at := time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC)
	times, _ := compute.NewTimes("Asia/Shanghai")
	cases := []struct {
		op       contract.Operation
		tables   []string
		response any
	}{
		{contract.StudentGrades, []string{"dwd_bzkscjzlb", "dwd_bzksjbsjzlb", "dwd_bzksxjjbsjzlb", "dwd_bzkskcsjzlb", "dwd_yxsdwjbsjzlb", "dwd_bzksbjsjzlb"}, contract.GradesResponse{}},
		{contract.BatchGrades, []string{"dwd_bzkscjzlb", "dwd_bzksjbsjzlb", "dwd_bzksxjjbsjzlb", "dwd_bzkskcsjzlb", "dwd_yxsdwjbsjzlb", "dwd_bzksbjsjzlb"}, contract.GradesResponse{}},
		{contract.StudentTimetable, []string{"dwd_bzkspksjzlb", "dwd_bzkskbsjzlb", "dwd_bzkskcsjzlb"}, contract.StudentTimetableResponse{StudentNumber: "001", AcademicYear: "2025-2026", Semester: "1"}},
		{contract.StaffTimetable, []string{"dwd_bzkspksjzlb", "dwd_bzkskcsjzlb"}, contract.StaffTimetableResponse{StaffNumber: "002", AcademicYear: "2025-2026", Semester: "1"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.op), func(t *testing.T) {
			deps, err := metadata.Dependencies(tc.op)
			if err != nil || !reflect.DeepEqual(deps, tc.tables) {
				t.Fatal(deps, err)
			}
			f := &facts{checks: map[string]time.Time{"unrelated": at.Add(-100 * time.Hour)}}
			for i, table := range tc.tables {
				f.checks[table] = at.Add(time.Duration(i) * time.Minute)
			}
			p := metadata.Provider{Store: f}
			got, err := p.SyncedAt(t.Context(), handle(), tc.op)
			if err != nil || !got.Equal(at) {
				t.Fatal(got, err)
			}
			payload, err := metadata.Payload(tc.response)
			if err != nil {
				t.Fatal(err)
			}
			response, err := metadata.Compose(tc.op, payload, got, times)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(response)
			if !strings.Contains(string(encoded), `"items":[]`) || !strings.Contains(string(encoded), `"synced_at":"2026-09-18T09:00:00+08:00"`) {
				t.Fatal(string(encoded))
			}
			delete(f.checks, tc.tables[len(tc.tables)-1])
			if _, err = p.SyncedAt(t.Context(), handle(), tc.op); !errors.Is(err, metadata.ErrMetadata) {
				t.Fatal(err)
			}
			f.err = errors.New("database unavailable")
			if _, err = p.SyncedAt(t.Context(), handle(), tc.op); !errors.Is(err, f.err) {
				t.Fatal(err)
			}
		})
	}
}

func TestT11_17RejectStaleEnvelopeAndMissingBusinessFields(t *testing.T) {
	times, _ := compute.NewTimes("Asia/Shanghai")
	for _, data := range []string{`{"total":0,"items":[],"synced_at":"old"}`, `{}`, `{"total":0,"items":null}`, `{"status":"success","data":{}}`} {
		if _, err := metadata.Compose(contract.StudentGrades, []byte(data), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), times); err == nil {
			t.Fatal(data)
		}
	}
}
