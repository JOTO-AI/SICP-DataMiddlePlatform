//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/adapters"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/staff"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/students"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/teaching"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type capacityMatcher struct {
	baselineMatcher
	calls, rows int
}

func (m *capacityMatcher) ReadMatching(ctx context.Context, h read.Handle, table, after string, n int, filters []source.Filter) ([]source.SourceRow, error) {
	m.calls++
	rows, err := m.baselineMatcher.ReadMatching(ctx, h, table, after, n, filters)
	m.rows += len(rows)
	return rows, err
}

// This entry measures service/SQL capacity. HTTP cache, publication and image
// upgrade are separate acceptance cases; this report cannot certify them.
func TestI11_Phase2FullScaleSourceQueries(t *testing.T) {
	e := newEnvironmentWithLocale(t, 12*time.Minute, "en_US.utf8")
	e.migrate(t)
	conn := e.connection(t, 0)
	_, err := conn.Exec(e.ctx, `
INSERT INTO source.dwd_bzksjbsjzlb ("XH","XM") SELECT lpad(i::text,6,'0'),'Synthetic Student' FROM generate_series(1,35000) i;
INSERT INTO source.dwd_bzksxjjbsjzlb ("XH","XZ") SELECT lpad(i::text,6,'0'),'3'::json FROM generate_series(1,35000) i;
INSERT INTO source.dwd_bzksgrtxsjzlb ("XH","YDDH") SELECT lpad(i::text,6,'0'),'synthetic' FROM generate_series(1,35000) i;
INSERT INTO source.dwd_jzgjcsjzlb ("JGH","XM") SELECT 'T'||lpad(i::text,6,'0'),'Synthetic Teacher' FROM generate_series(1,2600) i;
INSERT INTO source.dwd_bzkspksjzlb ("PKH","JXBH","JHKCH","KKXND","KKXQM","JSJGH")
SELECT lpad(i::text,9,'0'),CASE WHEN i<=6000 THEN 'HOT' ELSE 'J'||lpad(((i-6001)/60)::text,6,'0') END,
'C1','2026-2027','1',CASE WHEN i<=6000 THEN 'HOT_TEACHER' ELSE 'T'||lpad((((i-6001)/60)%2600+1)::text,6,'0') END FROM generate_series(1,540000) i;
INSERT INTO source.dwd_bzkskbsjzlb ("ID","JXBH","JHKCH","XH")
SELECT lpad(i::text,9,'0'),CASE WHEN i<=5000 THEN 'HOT' ELSE 'J'||lpad(((i-5001)%8900)::text,6,'0') END,
'C1',lpad(((i-1)%35000+1)::text,6,'0') FROM generate_series(1,490000) i;
ANALYZE source.dwd_bzkspksjzlb; ANALYZE source.dwd_bzkskbsjzlb;
`)
	if err != nil {
		t.Fatal(err)
	}
	m := &capacityMatcher{baselineMatcher: baselineMatcher{source.Repository{Registry: e.registry, Database: adapters.QueryDatabase{Database: e.db}}}}
	times, _ := compute.NewTimes("Asia/Shanghai")
	limits := read.DefaultLimits()
	h := read.Handle{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 1, Combination: "synthetic"}
	now := time.Now()
	studentService := students.Service{Source: m, Times: times, Clock: ports.SystemClock{}, Limits: limits}
	staffService := staff.Service{Source: m, Times: times, Clock: ports.SystemClock{}, Limits: limits}
	teachingService := teaching.Service{Source: m, Times: times, Clock: ports.SystemClock{}, Limits: limits}
	type sample struct {
		Name           string
		Milliseconds   int64
		SQL, Rows      int
		AllocatedBytes uint64
		Error          bool
	}
	samples := []sample{}
	plans := map[string]json.RawMessage{}
	defer func() {
		report := map[string]any{"schema": "phase2-capacity-v1", "commit": os.Getenv("GITHUB_SHA"), "passed": !t.Failed(), "seed": "sequential-v1", "students": 35000, "staff": 2600, "schedules": 540000, "relations": 490000, "hot_lessons": 6000, "hot_members": 5000, "limits": limits, "samples": samples, "explain": plans, "scope": "service SQL only; allocation delta is not peak RSS; no concurrency or cache certification"}
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		if err := os.MkdirAll("../../artifacts/integration", 0700); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile("../../artifacts/integration/I11-capacity.json", raw, 0600); err != nil {
			t.Error(err)
		}
	}()
	measure := func(name string, query func() error) {
		t.Helper()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		calls, rows, start := m.calls, m.rows, time.Now()
		err := query()
		runtime.ReadMemStats(&after)
		samples = append(samples, sample{name, time.Since(start).Milliseconds(), m.calls - calls, m.rows - rows, after.TotalAlloc - before.TotalAlloc, err != nil})
		if err != nil {
			t.Fatal(name, err)
		}
	}
	seen := map[string]bool{}
	for page := 1; page <= 35; page++ {
		measure("students-page", func() error {
			got, err := studentService.Query(e.ctx, h, contract.StudentsRequest{PaginationRequest: contract.PaginationRequest{Page: &page}}, now)
			if err != nil {
				return err
			}
			if got.Total != 35000 || len(got.Items) != 1000 {
				t.Fatal("student pagination")
			}
			for _, item := range got.Items {
				if seen[item.StudentNumber] {
					t.Fatal("duplicate student")
				}
				seen[item.StudentNumber] = true
			}
			return nil
		})
	}
	if len(seen) != 35000 {
		t.Fatal("missing students")
	}
	id := "035000"
	measure("student-exact", func() error {
		got, err := studentService.Query(e.ctx, h, contract.StudentsRequest{StudentNumber: &id}, now)
		if err == nil && (got.Total != 1 || got.Items[0].StudentNumber != id) {
			t.Fatal("exact lookup")
		}
		return err
	})
	for page := 1; page <= 3; page++ {
		measure("staff-page", func() error {
			got, err := staffService.Query(e.ctx, h, contract.StaffRequest{PaginationRequest: contract.PaginationRequest{Page: &page}}, now)
			want := 1000
			if page == 3 {
				want = 600
			}
			if err == nil && (got.Total != 2600 || len(got.Items) != want) {
				t.Fatal("staff pagination")
			}
			return err
		})
	}
	measure("hot-teacher", func() error {
		got, err := teachingService.Staff(e.ctx, h, contract.StaffTeachingClassesRequest{StaffNumber: "HOT_TEACHER", AcademicYear: "2026-2027", Semester: "1"}, now)
		if err == nil && (got.Total != 1 || got.Items[0].StudentCount != 5000) {
			t.Fatal("hot class count")
		}
		return err
	})
	seen = map[string]bool{}
	size := 1000
	for page := 1; page <= 5; page++ {
		measure("hot-roster", func() error {
			got, err := teachingService.Students(e.ctx, h, contract.TeachingClassStudentsRequest{PaginationRequest: contract.PaginationRequest{Page: &page, PageSize: &size}, TeachingClassNumber: "HOT", PlannedCourseCode: "C1", AcademicYear: "2026-2027", Semester: "1"}, now)
			if err != nil {
				return err
			}
			if got.Total != 5000 || len(got.Items) != 1000 {
				t.Fatal("roster pagination")
			}
			for _, item := range got.Items {
				if seen[item.StudentNumber] {
					t.Fatal("duplicate member")
				}
				seen[item.StudentNumber] = true
			}
			return nil
		})
	}
	if len(seen) != 5000 {
		t.Fatal("missing roster members")
	}
	for _, spec := range []struct{ table, pk string }{{"dwd_bzkspksjzlb", "PKH"}, {"dwd_bzkskbsjzlb", "ID"}} {
		var plan []byte
		// Identifiers are fixed literals above; values remain parameters.
		query := `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT * FROM source.` + spec.table + ` WHERE "JXBH" COLLATE "C"=ANY($1::text[]) AND "` + spec.pk + `" COLLATE "C">$2 ORDER BY "` + spec.pk + `" COLLATE "C" LIMIT 500`
		if err := conn.QueryRow(e.ctx, query, []string{"HOT"}, "").Scan(&plan); err != nil {
			t.Fatal(err)
		}
		plans[spec.table] = plan
	}
}
