package buildfixture_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildfixture"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

func fixture() buildfixture.Fixture {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return buildfixture.Fixture{Seed: 73, Small: true, Epoch: at, ChangedAt: at.Add(time.Hour)}
}
func TestBT01_07DeterministicIndependentDigestsAndAllTableChanges(t *testing.T) {
	f := fixture()
	a, err := f.Expected(context.Background(), "scope")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.Expected(context.Background(), "scope")
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("seed not deterministic")
	}
	count := 0
	for _, table := range buildfixture.Tables() {
		count += table.Rows
	}
	if count != buildfixture.TotalRows {
		t.Fatal(count)
	}
	for _, pct := range []int{1, 100} {
		f.Percent = pct
		b, err = f.Expected(context.Background(), "scope")
		if err != nil {
			t.Fatal(err)
		}
		for i, s := range b {
			if s.Changed < 1 || s.Bytes < 1 || s.SHA256 == a[i].SHA256 || pct == 100 && s.Changed != s.Rows {
				t.Fatal("missing expected replacement")
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = f.Expected(ctx, "scope"); err == nil {
		t.Fatal("reference ignored cancellation")
	}
}
func TestBT01_08DistributionAndIndependentCanonicalOracle(t *testing.T) {
	f := fixture()
	widths := map[int]bool{}
	nulls, strings, numbers, multi := 0, 0, 0, 0
	courses := map[string]int{}
	for _, table := range buildfixture.Tables() {
		for i := 0; i < f.Count(table); i++ {
			raw, err := f.Row(table, i)
			if err != nil {
				t.Fatal(err)
			}
			row, err := ingest.DecodeRecord(table.Name, raw)
			if err != nil {
				t.Fatalf("%s: %v", table.Name, err)
			}
			canonical, err := row.Canonical()
			if err != nil || !bytes.Equal(raw, canonical) {
				t.Fatal("independent JSON does not match input contract")
			}
			widths[len(raw)] = true
			for _, v := range row.Fields {
				if v.Type == ingest.Null {
					nulls++
				}
				if v.Type == ingest.String {
					strings++
				}
				if v.Type == ingest.Number {
					numbers++
				}
			}
			if table.Name == "dwd_bzkspksjzlb" {
				var r source.SourceRow
				if json.Unmarshal(raw, &r) != nil {
					t.Fatal("row")
				}
				teachers, err := timetable.ParseTeachers(r)
				if err != nil || len(teachers.StaffNumbers) != 2 {
					t.Fatal("multi-teacher distribution")
				}
				if _, err = timetable.ParseCalendar(r); err != nil {
					t.Fatal(err)
				}
				multi++
				courses[string(r["JHKCH"])]++
			}
		}
	}
	// Capacity distribution reuses each planned course in many schedule rows.
	full := f
	full.Small = false
	var schedules buildfixture.Table
	for _, table := range buildfixture.Tables() {
		if table.Name == "dwd_bzkspksjzlb" {
			schedules = table
		}
	}
	r1, _ := full.Row(schedules, 0)
	r2, _ := full.Row(schedules, 8160)
	var x, y map[string]any
	json.Unmarshal(r1, &x)
	json.Unmarshal(r2, &y)
	if nulls == 0 || strings == 0 || numbers == 0 || multi == 0 || len(widths) < 10 || x["JHKCH"] != y["JHKCH"] || bytes.Equal(r1, r2) {
		t.Fatal("degenerate distribution")
	}
}
