package merge_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/merge"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

const grades = "dwd_bzkscjzlb"

func records(t *testing.T, table string, rows ...string) []ingest.Record {
	t.Helper()
	result := make([]ingest.Record, 0, len(rows))
	for _, raw := range rows {
		r, err := ingest.DecodeRecord(table, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, r)
	}
	return result
}

func TestT04_06ObjectAndRecordOrderAreInsignificant(t *testing.T) {
	before := records(t, grades, `{"ID":"02","XF":{"b":2,"a":1}}`, `{"ID":"01","XF":3.0}`)
	after := records(t, grades, `{"XF":3.0,"ID":"01"}`, `{"XF":{"a":1,"b":2},"ID":"02"}`)
	report, err := merge.Compare(context.Background(), grades, before, after)
	if err != nil || !reflect.DeepEqual(report, merge.Report{BeforeRows: 2, AfterRows: 2, Changes: []merge.Change{}}) {
		t.Fatalf("reorder changed source: %+v %v", report, err)
	}
}

func TestT04_07And08SourceSemanticsRemainDistinct(t *testing.T) {
	for name, pair := range map[string][2]string{
		"array order":       {`{"ID":"01","XF":[1,2]}`, `{"ID":"01","XF":[2,1]}`},
		"space":             {`{"ID":"01","XH":"001"}`, `{"ID":"01","XH":" 001"}`},
		"ETL_TIME":          {`{"ID":"01","ETL_TIME":"2026-09-01 00:00:00"}`, `{"ID":"01","ETL_TIME":"2026-09-01 00:00:01"}`},
		"number token":      {`{"ID":"01","XF":3}`, `{"ID":"01","XF":3.0}`},
		"string number":     {`{"ID":"01","XF":3}`, `{"ID":"01","XF":"3"}`},
		"exponent token":    {`{"ID":"01","XF":3}`, `{"ID":"01","XF":3e0}`},
		"missing null":      {`{"ID":"01"}`, `{"ID":"01","XH":null}`},
		"missing JSON null": {`{"ID":"01"}`, `{"ID":"01","XF":null}`},
		"leading zero":      {`{"ID":"01","XH":"001"}`, `{"ID":"01","XH":"1"}`},
	} {
		t.Run(name, func(t *testing.T) {
			report, err := merge.Compare(context.Background(), grades, records(t, grades, pair[0]), records(t, grades, pair[1]))
			want := []merge.Change{{Key: "01", Kind: merge.Modified, BeforeRows: 1, AfterRows: 1}}
			if err != nil || !reflect.DeepEqual(report.Changes, want) {
				t.Fatalf("lost source difference: %+v %v", report, err)
			}
		})
	}
	missing, _ := value.Bind(value.Text, value.Field{})
	null, _ := value.Bind(value.Text, value.Field{Present: true, Raw: []byte("null")})
	if missing != nil || null != nil {
		t.Fatal("expected SQL NULL for both; comparison must retain source evidence")
	}
}

func TestT04_06DeterministicReportAndDuplicateMultiplicity(t *testing.T) {
	before := records(t, grades, `{"ID":"z"}`, `{"ID":"a","XF":1}`, `{"ID":"m"}`, `{"ID":"m"}`)
	after := records(t, grades, `{"ID":"b"}`, `{"ID":"m"}`, `{"ID":"a","XF":2}`)
	report, err := merge.Compare(context.Background(), grades, before, after)
	want := merge.Report{BeforeRows: 4, AfterRows: 3, Changes: []merge.Change{
		{Key: "a", Kind: merge.Modified, BeforeRows: 1, AfterRows: 1},
		{Key: "b", Kind: merge.Added, AfterRows: 1},
		{Key: "m", Kind: merge.Modified, BeforeRows: 2, AfterRows: 1},
		{Key: "z", Kind: merge.Removed, BeforeRows: 1},
	}}
	if err != nil || !reflect.DeepEqual(report, want) {
		t.Fatalf("report mismatch: %+v %v", report, err)
	}
}

func TestCompareRejectsUntrustedRecordsAndCancellation(t *testing.T) {
	r := records(t, grades, `{"ID":"01"}`)
	r[0].Key = "forged"
	if _, err := merge.Compare(context.Background(), grades, nil, r); !errors.Is(err, merge.ErrInvalid) {
		t.Fatal("forged identity accepted")
	}
	if _, err := merge.Compare(context.Background(), "not_a_table", nil, nil); !errors.Is(err, merge.ErrInvalid) {
		t.Fatal("unknown source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := merge.Compare(ctx, grades, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}
