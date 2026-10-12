package students_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/students"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/testkit"
)

// Generate ordered pages without retaining a second full-population fixture or
// performing a full fake-table scan for each source batch.
type population struct {
	t                            *testing.T
	count, calls, rows, contacts int
	handle                       read.Handle
}

func (p *population) ReadMatching(ctx context.Context, h read.Handle, table, after string, limit int, filters []source.Filter) ([]source.SourceRow, error) {
	p.t.Helper()
	p.calls++
	if h != p.handle {
		p.t.Fatal("handle changed")
	}
	if _, ok := ctx.Deadline(); !ok {
		p.t.Fatal("SQL has no deadline")
	}
	keys := []string{}
	if table == students.ContactsTable {
		if len(filters) != 1 || filters[0].Column != "XH" || filters[0].Values == nil {
			p.t.Fatal("contact scan was not bounded", filters)
		}
		values := filters[0].Values
		start := sort.SearchStrings(values, after)
		for start < len(values) && values[start] <= after {
			start++
		}
		end := min(start+limit, len(values))
		keys = append(keys, values[start:end]...)
	} else {
		if table != students.Table && table != students.EnrollmentsTable {
			p.t.Fatal(table)
		}
		if len(filters) != 0 {
			p.t.Fatal(filters)
		}
		offset, _ := strconv.Atoi(after)
		for i := offset + 1; i <= min(offset+limit, p.count); i++ {
			keys = append(keys, fmt.Sprintf("%06d", i))
		}
	}
	rows := make([]source.SourceRow, 0, len(keys))
	for _, id := range keys {
		raw, _ := json.Marshal(id)
		row := source.SourceRow{"XH": raw}
		// PostgreSQL returns all catalog columns, including explicit nulls.
		for _, spec := range source.Catalog() {
			if spec.Name == table {
				for _, column := range spec.Columns {
					if column.Name != "XH" {
						row[column.Name] = json.RawMessage(`null`)
					}
				}
			}
		}
		switch table {
		case students.Table:
			row["XM"] = json.RawMessage(`"Synthetic Student"`)
		case students.EnrollmentsTable:
			row["XZ"] = json.RawMessage(`3`)
			row["XSDQZTM"] = json.RawMessage(`"unknown"`)
		case students.ContactsTable:
			row["YDDH"] = json.RawMessage(`"synthetic"`)
			p.contacts++
		}
		rows = append(rows, row)
	}
	p.rows += len(rows)
	return rows, nil
}

func TestT24_07FullPopulationUsesFrozenForegroundBudget(t *testing.T) {
	limits := read.DefaultLimits()
	if limits.Rows != 100000 || limits.Batches != 1000 || limits.Bytes != 64<<20 || limits.BatchSize != 500 || limits.Duration != 20*time.Second || limits.BatchBytes != 0 {
		t.Fatal("foreground limits changed", limits)
	}
	h := read.Handle{Position: storage.Position{Instance: storage.Main, Slot: 0}, Generation: 1, Combination: "synthetic"}
	p := &population{t: t, count: 35000, handle: h}
	times, _ := compute.NewTimes("Asia/Shanghai")
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	svc := students.Service{Source: p, Times: times, Clock: testkit.FakeClock{Time: at}, Limits: limits}
	page := 35
	got, err := svc.Query(t.Context(), h, contract.StudentsRequest{PaginationRequest: contract.PaginationRequest{Page: &page}}, at)
	if err != nil || got.Total != 35000 || len(got.Items) != 1000 || got.Items[0].StudentNumber != "034001" || got.Items[999].StudentNumber != "035000" || p.rows != 71000 || p.contacts != 1000 || p.calls != 145 {
		t.Fatal(got.Total, len(got.Items), p.rows, p.contacts, p.calls, err)
	}
	// Wide or larger source sets still fail under fixed limits; no partial page.
	p = &population{t: t, count: 35000, handle: h}
	svc.Source = p
	svc.Limits.Bytes = 1024
	got, err = svc.Query(t.Context(), h, contract.StudentsRequest{}, at)
	if err == nil || got.Items != nil || got.Total != 0 {
		t.Fatal("byte budget bypassed", got.Total, err)
	}
}
