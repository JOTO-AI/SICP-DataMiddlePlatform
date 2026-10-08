package candidate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

func TestBT04_ConsumerValidationReducesScansAndRejectsLateMutation(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		input, files, options, root := targetInput(t, ingest.Full, `{"ID":"001","XH":"0001","XF":3.0,"JD":"3","KCCJ":null}`)
		m, err := files.Prepare(t.Context(), options, input)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := files.document(t.Context(), m)
		if err != nil {
			t.Fatal(err)
		}
		var chunk string
		for _, table := range doc.Tables {
			if table.Version.Table == "dwd_bzkscjzlb" {
				chunk = table.Chunks[0].Name
			}
		}
		record, err := ingest.DecodeRecord("dwd_bzkscjzlb", []byte(`{"ID":"001","XH":"0001","XF":3.0,"JD":"3","KCCJ":null}`))
		if err != nil {
			t.Fatal(err)
		}
		actual := source.SourceRow{}
		for _, table := range source.Catalog() {
			if table.Name == "dwd_bzkscjzlb" {
				for _, col := range table.Columns {
					token, present := record.Fields[col.Name]
					v, err := value.Bind(col.Kind, value.Field{Present: present, Raw: token.Raw})
					if err != nil {
						t.Fatal(err)
					}
					actual[col.Name], err = value.Restore(col.Kind, v)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		// Mutation happens on a later table, after the grade chunk was consumed.
		reader := &lateMutationMatcher{row: actual, mutate: func() {
			if corrupt {
				if err := os.WriteFile(filepath.Join(root, chunk), []byte("corrupt\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}}
		ctx, metrics := buildmetric.Begin(t.Context())
		checks := StorageChecks{Target: files, Source: reader, Clock: ports.SystemClock{}, Limits: read.BackgroundLimits(time.Minute), BackgroundTimeout: time.Minute}
		err = checks.SourceStorage(ctx, read.Handle{Position: storage.Position{Instance: storage.Main}, Generation: 1, Combination: m.Combination}, m)
		if corrupt && err == nil {
			t.Fatal("read-then-replaced chunk accepted")
		}
		if !corrupt && (err != nil || metrics.Snapshot().Decodes != 2 || metrics.Snapshot().FileValidations != 2) {
			t.Fatal("duplicate scans/decodes remain", metrics.Snapshot(), err)
		}
	}
}

type lateMutationMatcher struct {
	row    source.SourceRow
	mutate func()
	done   bool
}

func (r *lateMutationMatcher) ReadMatching(_ context.Context, _ read.Handle, table, after string, _ int, _ []source.Filter) ([]source.SourceRow, error) {
	if table == "dwd_bzkscjzlb" && after == "" {
		return []source.SourceRow{r.row}, nil
	}
	if table > "dwd_bzkscjzlb" && !r.done {
		r.done = true
		r.mutate()
	}
	return nil, nil
}

func TestBT04_SessionBindingAndExpiry(t *testing.T) {
	input, files, options, _ := targetInput(t, ingest.Full, `{"ID":"001"}`)
	m, err := files.Prepare(t.Context(), options, input)
	if err != nil {
		t.Fatal(err)
	}
	s, closeSession, err := files.session(t.Context(), m)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSession()
	other := m
	other.Combination = "another"
	if _, err = s.Open(t.Context(), other, m.Tables[0]); err == nil {
		t.Fatal("cross-combination session")
	}
	rows, err := s.Open(t.Context(), m, m.Tables[0])
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close(t.Context())
	closeSession()
	if _, err = rows.Next(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatal("expired iterator", err)
	}
	if err = s.Validate(t.Context(), m); !errors.Is(err, context.Canceled) {
		t.Fatal("expired evidence", err)
	}
}
